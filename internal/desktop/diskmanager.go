package desktop

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"

	"win95linux/internal/disk"
	"win95linux/internal/ui"
)

const maxDiskTableBackupBytes = 8 << 20

type diskManagerOp int

const (
	diskManagerCreate diskManagerOp = iota
	diskManagerDelete
	diskManagerResize
	diskManagerFormat
	diskManagerBackup
	diskManagerRestore
)

func (op diskManagerOp) label() string {
	switch op {
	case diskManagerCreate:
		return "创建分区"
	case diskManagerDelete:
		return "删除分区"
	case diskManagerResize:
		return "调整分区"
	case diskManagerFormat:
		return "格式化分区"
	case diskManagerBackup:
		return "备份分区表"
	case diskManagerRestore:
		return "恢复分区表"
	default:
		return "磁盘操作"
	}
}

func (op diskManagerOp) destructive() bool {
	return op != diskManagerBackup
}

type diskManagerDiskRow struct {
	info          diskInfo
	device        *disk.Device
	mounts        []diskMountInfo
	partMount     map[int]diskMountInfo
	filesys       map[int]string
	protected     string
	protectedText diskUIText
	err           string
	errText       diskUIText
}

type diskOperationTarget struct {
	path         string
	device       disk.Device
	partition    *disk.Partition
	spec         disk.PartitionSpec
	filesystem   string
	backup       disk.Backup
	backupPath   string
	system       bool
	operation    diskManagerOp
	operationSet bool
}

// DiskManagerApp is a deliberately explicit disk editor. No disk or partition
// is selected after a refresh, and mounted system/data disks are never editable.
type DiskManagerApp struct {
	Base
	d          *Desktop
	win        *Window
	rows       []diskManagerDiskRow
	diskSel    int
	partSel    int
	diskChosen bool
	partChosen bool
	diskScroll VScroll
	partScroll VScroll
	busy       bool
	closed     bool
	status     string
	err        string

	listFn    func() ([]diskInfo, error)
	inspectFn func(string, ...disk.Option) (*disk.Device, error)
	mountsFn  func() ([]diskMountInfo, error)
	prepareFn func(diskOperationTarget) (diskOperationTarget, error)
	runFn     func(diskManagerOp, diskOperationTarget) error

	diskListR, partListR                ui.Rect
	diskScrollR, partScrollR            ui.Rect
	diskRowRects, partRowRects          []ui.Rect
	refreshR, createR, deleteR, resizeR ui.Rect
	formatR, backupR, restoreR, closeR  ui.Rect
	press                               string
}

func newDiskManagerApp(d *Desktop) *DiskManagerApp {
	a := &DiskManagerApp{
		d: d, diskSel: -1, partSel: -1,
		status: d.tr("正在读取磁盘…"),
		listFn: listDisks, inspectFn: disk.Inspect, mountsFn: readDiskMountInfo,
		runFn: executeDiskOperation,
	}
	a.prepareFn = a.prepareOperation
	return a
}

// openDiskManager is the integration entry point. Menu wiring is intentionally
// left to the shell owner so this change cannot alter existing menus.
func (d *Desktop) openDiskManager() {
	for _, w := range d.windows {
		if _, ok := w.App.(*DiskManagerApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := newDiskManagerApp(d)
	w := d.Open(a)
	a.win = w
	a.refresh()
}

func (a *DiskManagerApp) Title() string           { return a.d.tr("磁盘管理") }
func (a *DiskManagerApp) Icon() ui.IconKind       { return ui.IcoDrive }
func (a *DiskManagerApp) DefaultSize() (int, int) { return 880, 570 }
func (a *DiskManagerApp) MinSize() (int, int)     { return 760, 500 }
func (a *DiskManagerApp) Resizable() bool         { return true }

func (a *DiskManagerApp) RequestClose(d *Desktop, w *Window) bool {
	if a.busy {
		d.MessageBox(d.tr("磁盘管理"), d.tr("磁盘操作正在进行中，请等待完成后再关闭。"), IconWarning, []string{d.tr("确定")}, nil)
		return false
	}
	return true
}

func (a *DiskManagerApp) Close() { a.closed = true }

func (a *DiskManagerApp) post(f func()) {
	if a.d != nil {
		a.d.Post(f)
	}
}

type diskUIText struct {
	key  string
	args []any
}

func (t diskUIText) source() string {
	if t.key == "" {
		return ""
	}
	return fmt.Sprintf(t.key, t.args...)
}

func (t diskUIText) localized(d *Desktop) string {
	if t.key == "" {
		return ""
	}
	args := make([]any, len(t.args))
	for i, arg := range t.args {
		if size, ok := arg.(diskSizeValue); ok {
			args[i] = size.localized(d)
		} else {
			args[i] = arg
		}
	}
	return d.trf(t.key, args...)
}

type diskSizeValue uint64

func (n diskSizeValue) String() string { return diskSizeText(uint64(n)) }
func (n diskSizeValue) localized(d *Desktop) string {
	return diskSizeTextForUI(d, uint64(n))
}

type diskRefreshResult struct {
	rows       []diskManagerDiskRow
	statusText diskUIText
	err        error
}

func (a *DiskManagerApp) refresh() {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.status = a.d.tr("正在读取磁盘和挂载状态…")
	a.err = ""
	a.diskSel, a.partSel = -1, -1
	a.diskChosen, a.partChosen = false, false
	a.d.Invalidate()
	done := make(chan diskRefreshResult, 1)
	go func() {
		rows, status, err := loadDiskManagerRowsWithStatus(a.listFn, a.inspectFn, a.mountsFn)
		done <- diskRefreshResult{rows: rows, statusText: status, err: err}
	}()
	go func() {
		res := <-done
		a.post(func() {
			if a.closed {
				return
			}
			a.busy = false
			a.rows = res.rows
			a.status = res.statusText.localized(a.d)
			a.err = ""
			if res.err != nil {
				a.err = res.err.Error()
			}
			a.diskScroll.Total = len(a.rows)
			a.diskScroll.Pos = 0
			a.d.Invalidate()
		})
	}()
}

func loadDiskManagerRows(listFn func() ([]diskInfo, error), inspectFn func(string, ...disk.Option) (*disk.Device, error), mountsFn func() ([]diskMountInfo, error)) ([]diskManagerDiskRow, string, error) {
	rows, status, err := loadDiskManagerRowsWithStatus(listFn, inspectFn, mountsFn)
	return rows, status.source(), err
}

func loadDiskManagerRowsWithStatus(listFn func() ([]diskInfo, error), inspectFn func(string, ...disk.Option) (*disk.Device, error), mountsFn func() ([]diskMountInfo, error)) ([]diskManagerDiskRow, diskUIText, error) {
	infos, err := listFn()
	if err != nil {
		return nil, diskUIText{key: "磁盘读取失败"}, err
	}
	mounts, mountErr := mountsFn()
	rows := make([]diskManagerDiskRow, 0, len(infos))
	for _, info := range infos {
		row := diskManagerDiskRow{info: info, filesys: make(map[int]string)}
		if mountErr != nil {
			row.errText = diskUIText{key: "挂载状态检查失败: %s", args: []any{mountErr.Error()}}
			row.err = row.errText.source()
			rows = append(rows, row)
			continue
		}
		row.mounts = mounts
		dev, inspectErr := inspectFn(info.path)
		if inspectErr != nil {
			row.err = inspectErr.Error()
			rows = append(rows, row)
			continue
		}
		row.device = dev
		if info.sizeBytes < 0 || dev.Size != uint64(info.sizeBytes) {
			if info.sizeBytes < 0 {
				row.errText = diskUIText{key: "设备容量不一致: 列表无效容量，分区表 %s", args: []any{diskSizeValue(dev.Size)}}
			} else {
				row.errText = diskUIText{key: "设备容量不一致: 列表 %s，分区表 %s", args: []any{diskSizeValue(info.sizeBytes), diskSizeValue(dev.Size)}}
			}
			row.err = row.errText.source()
			rows = append(rows, row)
			continue
		}
		safety := inspectDiskSafety(info.path, dev, mounts, diskSysBlockOwners, info.installer)
		row.protectedText = safety.protected
		row.protected = safety.protected.source()
		if safety.err != nil {
			row.err = safety.err.Error()
			rows = append(rows, row)
			continue
		}
		row.partMount = mountedDiskPartitions(info.path, dev, mounts)
		for _, p := range dev.Partitions {
			row.filesys[p.Number] = diskFilesystemLabel(info.path, p, row.partMount)
		}
		rows = append(rows, row)
	}
	status := diskUIText{key: "共 %d 块磁盘；默认未选择任何磁盘", args: []any{len(rows)}}
	if mountErr != nil {
		return rows, status, storageWrapErrorf("无法验证挂载状态，已禁用编辑: %s", mountErr)
	}
	return rows, status, nil
}

func (a *DiskManagerApp) selectedDisk() *diskManagerDiskRow {
	if !a.diskChosen || a.diskSel < 0 || a.diskSel >= len(a.rows) {
		return nil
	}
	row := &a.rows[a.diskSel]
	if row.device == nil || row.err != "" {
		return nil
	}
	return row
}

func (a *DiskManagerApp) selectedPartition(row *diskManagerDiskRow) *disk.Partition {
	if row == nil || !a.partChosen || a.partSel < 0 || a.partSel >= len(row.device.Partitions) {
		return nil
	}
	p := row.device.Partitions[a.partSel]
	return &p
}

func (a *DiskManagerApp) canModify() bool {
	row := a.selectedDisk()
	return !a.busy && row != nil && row.protected == ""
}

func (a *DiskManagerApp) canBackup() bool {
	return !a.busy && a.selectedDisk() != nil
}

func diskSizeText(n uint64) string {
	if n > uint64(^uint64(0)>>1) {
		return strconv.FormatUint(n, 10) + " 字节"
	}
	return formatBytes(int64(n))
}

func diskSizeTextForUI(d *Desktop, n uint64) string {
	if n > uint64(^uint64(0)>>1) {
		return d.trf("%d 字节", n)
	}
	return storageSizeText(d, int64(n))
}

func diskPartitionDevicePath(path string, number int) (string, error) {
	if number <= 0 {
		return "", storageErrorf("分区号必须大于零")
	}
	base := filepath.Base(path)
	separator := ""
	switch runtime.GOOS {
	case "linux":
		for _, prefix := range []string{"nvme", "mmcblk", "loop", "md"} {
			if strings.HasPrefix(base, prefix) {
				separator = "p"
				break
			}
		}
	case "darwin":
		separator = "s"
	default:
		return "", storageErrorf("当前平台不支持分区设备命名: %s", runtime.GOOS)
	}
	return path + separator + strconv.Itoa(number), nil
}

type diskMountInfo struct {
	Source string
	Target string
	FS     string
	DevID  string
	Major  uint64
	Minor  uint64
}

func parseDiskMountInfo(data []byte) ([]diskMountInfo, error) {
	var mounts []diskMountInfo
	for lineNo, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return nil, storageErrorf("挂载表第 %d 行格式无效", lineNo+1)
		}
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			return nil, storageErrorf("挂载表第 %d 行缺少分隔符", lineNo+1)
		}
		majorText, minorText, ok := strings.Cut(fields[2], ":")
		if !ok || majorText == "" || minorText == "" || strings.Contains(minorText, ":") {
			return nil, storageErrorf("挂载表第 %d 行设备号无效", lineNo+1)
		}
		major, err := strconv.ParseUint(majorText, 10, 32)
		if err != nil {
			return nil, storageErrorf("挂载表第 %d 行设备号无效", lineNo+1)
		}
		minor, err := strconv.ParseUint(minorText, 10, 32)
		if err != nil {
			return nil, storageErrorf("挂载表第 %d 行设备号无效", lineNo+1)
		}
		mounts = append(mounts, diskMountInfo{
			Source: unescapeDiskMount(fields[sep+2]),
			Target: unescapeDiskMount(fields[4]),
			FS:     fields[sep+1],
			DevID:  strconv.FormatUint(major, 10) + ":" + strconv.FormatUint(minor, 10),
			Major:  major,
			Minor:  minor,
		})
	}
	return mounts, nil
}

func unescapeDiskMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func readDiskMountInfo() ([]diskMountInfo, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	return parseDiskMountInfo(data)
}

type diskDeviceRef struct {
	DevID string
	Name  string
}

func diskSysBlockOwners(devID string) []diskDeviceRef {
	return diskSysBlockOwnersSeen(devID, make(map[string]bool))
}

func diskSysBlockOwnersSeen(devID string, seen map[string]bool) []diskDeviceRef {
	if seen[devID] {
		return nil
	}
	seen[devID] = true
	resolved, err := filepath.EvalSymlinks(filepath.Join("/sys/dev/block", devID))
	if err != nil {
		return nil
	}
	marker := string(filepath.Separator) + "block" + string(filepath.Separator)
	idx := strings.LastIndex(resolved, marker)
	if idx < 0 {
		return nil
	}
	tail := resolved[idx+len(marker):]
	name, _, _ := strings.Cut(tail, string(filepath.Separator))
	if name == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join("/sys/class/block", name, "dev"))
	if err != nil {
		return nil
	}
	ownerID, ok := parseDiskDeviceID(strings.TrimSpace(string(data)))
	if !ok {
		return nil
	}
	owners := []diskDeviceRef{{DevID: ownerID, Name: name}}
	entries, err := os.ReadDir(filepath.Join("/sys/class/block", name, "slaves"))
	if err == nil {
		for _, entry := range entries {
			data, readErr := os.ReadFile(filepath.Join("/sys/class/block", entry.Name(), "dev"))
			if readErr != nil {
				return nil
			}
			slaveID, ok := parseDiskDeviceID(strings.TrimSpace(string(data)))
			if !ok {
				return nil
			}
			slaves := diskSysBlockOwnersSeen(slaveID, seen)
			if len(slaves) == 0 {
				return nil
			}
			owners = append(owners, slaves...)
		}
	}
	return owners
}

func parseDiskDeviceID(id string) (string, bool) {
	majorText, minorText, ok := strings.Cut(id, ":")
	if !ok || majorText == "" || minorText == "" || strings.Contains(minorText, ":") {
		return "", false
	}
	major, err := strconv.ParseUint(majorText, 10, 32)
	if err != nil {
		return "", false
	}
	minor, err := strconv.ParseUint(minorText, 10, 32)
	if err != nil {
		return "", false
	}
	return strconv.FormatUint(major, 10) + ":" + strconv.FormatUint(minor, 10), true
}

func diskSourceMatchesPath(source, path string) bool {
	source = filepath.Clean(source)
	path = filepath.Clean(path)
	if source == path {
		return true
	}
	if !filepath.IsAbs(source) {
		return false
	}
	base := filepath.Base(path)
	sourceBase := filepath.Base(source)
	for _, separator := range []string{"p", "s"} {
		prefix := base + separator
		if strings.HasPrefix(sourceBase, prefix) {
			suffix := strings.TrimPrefix(sourceBase, prefix)
			_, err := strconv.ParseUint(suffix, 10, 32)
			return err == nil
		}
	}
	return false
}

type diskSafetyResult struct {
	protected diskUIText
	err       error
}

func diskSafetyStatus(path string, dev *disk.Device, mounts []diskMountInfo, owners func(string) []diskDeviceRef, installer bool) (string, error) {
	res := inspectDiskSafety(path, dev, mounts, owners, installer)
	return res.protected.source(), res.err
}

func inspectDiskSafety(path string, dev *disk.Device, mounts []diskMountInfo, owners func(string) []diskDeviceRef, installer bool) diskSafetyResult {
	base := filepath.Base(path)
	for _, mount := range mounts {
		if mount.Major == 0 {
			continue
		}
		matched := diskSourceMatchesPath(mount.Source, path) || sameDiskNode(mount.Source, path)
		if dev != nil && dev.DeviceID != "" && mount.DevID == dev.DeviceID {
			matched = true
		}
		mountOwners := owners(mount.DevID)
		if !matched {
			for _, owner := range mountOwners {
				if (dev != nil && dev.DeviceID != "" && owner.DevID == dev.DeviceID) || owner.Name == base {
					matched = true
					break
				}
			}
		}
		if matched {
			return diskSafetyResult{protected: diskUIText{
				key:  "受保护：系统/数据盘已挂载于 %s（%s）",
				args: []any{mount.Target, mount.FS},
			}}
		}
		if len(mountOwners) == 0 {
			return diskSafetyResult{err: storageErrorf("无法确定挂载设备 %s（%s）所属磁盘；为安全起见拒绝编辑", mount.Source, mount.DevID)}
		}
	}
	if installer {
		return diskSafetyResult{protected: diskUIText{key: "受保护：系统安装盘"}}
	}
	return diskSafetyResult{}
}

func sameDiskNode(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func mountedDiskPartitions(path string, dev *disk.Device, mounts []diskMountInfo) map[int]diskMountInfo {
	out := make(map[int]diskMountInfo)
	for _, p := range dev.Partitions {
		partitionPath, err := diskPartitionDevicePath(path, p.Number)
		if err != nil {
			continue
		}
		for _, mount := range mounts {
			if diskSourceMatchesPath(mount.Source, partitionPath) || sameDiskNode(mount.Source, partitionPath) {
				out[p.Number] = mount
				break
			}
		}
	}
	return out
}

func diskFilesystemLabel(diskPath string, p disk.Partition, mounts map[int]diskMountInfo) string {
	if mount, ok := mounts[p.Number]; ok {
		return mount.FS
	}
	partitionPath, err := diskPartitionDevicePath(diskPath, p.Number)
	if err == nil {
		if fs := detectDiskFilesystem(partitionPath); fs != "" {
			return fs
		}
	}
	if p.Table == disk.TableGPT {
		switch strings.ToUpper(p.TypeGUID) {
		case disk.TypeESP:
			return "FAT (EFI)"
		case disk.TypeLinuxData:
			return "Linux"
		}
	}
	if p.Table == disk.TableMBR {
		switch p.Type {
		case 0x83:
			return "Linux"
		case 0x07:
			return "NTFS/exFAT"
		case 0x0b, 0x0c:
			return "FAT32"
		case 0x05, 0x0f, 0x85:
			return "扩展分区"
		}
	}
	return "未知"
}

func detectDiskFilesystem(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 1024)
	if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	if len(buf) >= 88 && string(buf[3:11]) == "EXFAT   " {
		return "exFAT"
	}
	if len(buf) >= 11 && string(buf[3:11]) == "NTFS    " {
		return "NTFS"
	}
	if len(buf) >= 512 && buf[510] == 0x55 && buf[511] == 0xaa {
		if len(buf) >= 90 && string(buf[82:90]) == "FAT32   " {
			return "FAT32"
		}
		if len(buf) >= 62 && string(buf[54:62]) == "FAT16   " {
			return "FAT16"
		}
		if len(buf) >= 62 && string(buf[54:62]) == "FAT12   " {
			return "FAT12"
		}
	}
	magic := make([]byte, 2)
	if _, err := f.ReadAt(magic, 1024+56); err == nil && binary.LittleEndian.Uint16(magic) == 0xef53 {
		features := make([]byte, 4)
		if _, err := f.ReadAt(features, 1024+96); err == nil {
			incompat := binary.LittleEndian.Uint32(features)
			if incompat&0x40 != 0 || incompat&0x80 != 0 || incompat&0x200 != 0 {
				return "ext4"
			}
		}
		compat := make([]byte, 4)
		if _, err := f.ReadAt(compat, 1024+92); err == nil && binary.LittleEndian.Uint32(compat)&0x4 != 0 {
			return "ext3"
		}
		return "ext2"
	}
	return ""
}

func sameDiskGeometry(want, got *disk.Device) bool {
	return want != nil && got != nil && want.DeviceID == got.DeviceID && want.Size == got.Size &&
		want.SectorSize == got.SectorSize && want.TotalSectors == got.TotalSectors && want.Table == got.Table
}

func sameDiskPartition(want, got *disk.Partition) bool {
	return want != nil && got != nil && want.Number == got.Number && want.Table == got.Table &&
		want.StartSector == got.StartSector && want.EndSector == got.EndSector &&
		want.SectorSize == got.SectorSize && want.Bytes() == got.Bytes()
}

func (a *DiskManagerApp) prepareOperation(target diskOperationTarget) (diskOperationTarget, error) {
	dev, err := a.inspectFn(target.path)
	if err != nil {
		return target, storageWrapErrorf("重新检查设备失败: %s", err)
	}
	info, err := os.Stat(target.path)
	if err != nil {
		return target, storageWrapErrorf("检查设备类型失败: %s", err)
	}
	if info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 {
		return target, storageErrorf("拒绝编辑非块设备 %s", target.path)
	}
	if !sameDiskGeometry(&target.device, dev) {
		return target, storageErrorf("设备几何或分区表已变化，请刷新后重新选择")
	}
	mounts, err := a.mountsFn()
	if err != nil {
		return target, storageWrapErrorf("无法确认挂载状态: %s", err)
	}
	protected, err := diskSafetyStatus(target.path, dev, mounts, diskSysBlockOwners, target.system)
	if err != nil {
		return target, err
	}
	if target.device.Table == disk.TableGPT && dev.GPT == nil {
		return target, storageErrorf("GPT 元数据缺失")
	}
	if opNeedsPartition(target) {
		p, ok := findDesktopPartition(dev, target.partition.Number)
		if !ok || !sameDiskPartition(target.partition, &p) {
			return target, storageErrorf("所选分区已变化，请刷新后重新选择")
		}
		target.partition = &p
	}
	if target.op() == diskManagerRestore {
		if target.backup.Device.SectorSize != dev.SectorSize || target.backup.Device.TotalSectors != dev.TotalSectors || target.backup.Device.Size != dev.Size {
			return target, storageErrorf("备份与目标设备的容量或逻辑扇区不匹配")
		}
	}
	if target.op().destructive() && protected != "" {
		return target, storageErrorf("已拒绝操作: %s", protected)
	}
	target.device = *dev
	return target, nil
}

func (target diskOperationTarget) op() diskManagerOp {
	if target.operationSet {
		return target.operation
	}
	switch {
	case target.backupPath != "" && target.backup.Version == 0:
		return diskManagerBackup
	case target.backup.Version != 0:
		return diskManagerRestore
	case target.partition != nil && target.filesystem != "":
		return diskManagerFormat
	case target.partition != nil && target.spec.SizeSectors != 0:
		return diskManagerResize
	case target.partition != nil:
		return diskManagerDelete
	default:
		return diskManagerCreate
	}
}

func opNeedsPartition(target diskOperationTarget) bool {
	op := target.op()
	return op == diskManagerDelete || op == diskManagerResize || op == diskManagerFormat
}

func findDesktopPartition(dev *disk.Device, number int) (disk.Partition, bool) {
	for _, p := range dev.Partitions {
		if p.Number == number {
			return p, true
		}
	}
	return disk.Partition{}, false
}

func executeDiskOperation(op diskManagerOp, target diskOperationTarget) error {
	opts := []disk.Option{disk.Options{ExpectedDeviceID: target.device.DeviceID}}
	switch op {
	case diskManagerCreate:
		return disk.CreatePartition(target.path, target.spec, opts...)
	case diskManagerDelete:
		return disk.DeletePartition(target.path, target.partition.Number, opts...)
	case diskManagerResize:
		return disk.ResizePartition(target.path, target.partition.Number, target.spec.EndSector, opts...)
	case diskManagerFormat:
		return disk.FormatPartition(target.path, target.partition.Number, target.filesystem, opts...)
	case diskManagerBackup:
		_, err := disk.BackupTable(target.path, target.backupPath)
		return err
	case diskManagerRestore:
		return disk.RestoreTable(target.path, target.backup, opts...)
	default:
		return errors.New("未知磁盘操作")
	}
}

type diskPrepareResult struct {
	target diskOperationTarget
	err    error
}

func (a *DiskManagerApp) requestOperation(op diskManagerOp, target diskOperationTarget, detail string, proceed func(diskManagerOp, diskOperationTarget)) {
	if a.busy || a.closed {
		return
	}
	target.operation, target.operationSet = op, true
	a.busy = true
	a.status = a.d.tr("正在校验设备几何和挂载状态…")
	a.err = ""
	a.d.Invalidate()
	done := make(chan diskPrepareResult, 1)
	go func() {
		prepared, err := a.prepareFn(target)
		done <- diskPrepareResult{target: prepared, err: err}
	}()
	go func() {
		res := <-done
		a.post(func() {
			if a.closed {
				return
			}
			if res.err != nil {
				a.busy = false
				a.err = res.err.Error()
				a.status = a.d.tr("校验失败，未执行任何操作")
				a.d.MessageBox(a.d.tr("已拒绝磁盘操作"), res.err.Error(), IconWarning, []string{a.d.tr("确定")}, nil)
				a.d.Invalidate()
				return
			}
			confirmDiskOperation(a.d, op, res.target, detail, func() {
				a.busy = false
				proceed(op, res.target)
			}, func() {
				a.busy = false
				a.status = a.d.tr("已取消，未执行任何操作")
				a.d.Invalidate()
			})
		})
	}()
}

func confirmDiskOperation(d *Desktop, op diskManagerOp, target diskOperationTarget, detail string, proceed func(), cancel func()) {
	label := d.tr(op.label())
	title := d.trf("危险操作：%s", label)
	text := d.trf("%s\n\n设备：%s\n容量：%s\n分区表：%s\n\n%s",
		detail, target.path, diskSizeTextForUI(d, target.device.Size), strings.ToUpper(target.device.Table), d.tr(diskOperationImpact(op)))
	d.MessageBox(title, text, IconWarning, []string{d.tr("继续"), d.tr("取消")}, func(i int) {
		if i != 0 {
			cancel()
			return
		}
		d.MessageBox(d.trf("最后确认：%s", label), text+d.tr("\n\n这是第二次确认。只有再次点击“确认执行”才会修改设备。"), IconWarning, []string{d.tr("确认执行"), d.tr("取消")}, func(j int) {
			if j != 0 {
				cancel()
				return
			}
			proceed()
		})
	})
}

func diskOperationImpact(op diskManagerOp) string {
	switch op {
	case diskManagerCreate:
		return "将写入新的分区条目；不会自动创建文件系统。"
	case diskManagerDelete:
		return "只清除分区表条目，分区数据不会立即擦除，但之后可能被覆盖。"
	case diskManagerResize:
		return "只改变分区表结束位置；不会自动调整文件系统大小。"
	case diskManagerFormat:
		return "将擦除所选分区上的文件系统和数据，通常无法恢复。"
	case diskManagerBackup:
		return "只备份分区表元数据，不包含分区文件内容；目标文件若存在会被替换。"
	case diskManagerRestore:
		return "将用备份覆盖当前分区表；分区内容不会被恢复，现有分区布局可能失效。"
	default:
		return ""
	}
}

type diskOperationResult struct {
	err error
}

func (a *DiskManagerApp) startOperation(op diskManagerOp, target diskOperationTarget) {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.status = a.d.trf("正在%s…", a.d.tr(op.label()))
	a.err = ""
	a.d.Invalidate()
	done := make(chan diskOperationResult, 1)
	go func() {
		prepared, err := a.prepareFn(target)
		if err == nil {
			err = a.runFn(op, prepared)
		}
		done <- diskOperationResult{err: err}
	}()
	go func() {
		res := <-done
		a.post(func() {
			if a.closed {
				return
			}
			a.busy = false
			label := a.d.tr(op.label())
			if res.err != nil {
				a.err = res.err.Error()
				a.status = a.d.trf("%s失败", label)
				a.d.MessageBox(a.d.trf("%s失败", label), res.err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			} else {
				a.err = ""
				a.status = a.d.trf("%s完成", label)
				a.d.MessageBox(a.d.trf("%s完成", label), a.d.trf("设备 %s 的%s已成功。", target.path, label), IconInfo, []string{a.d.tr("确定")}, nil)
				a.refresh()
			}
			a.d.Invalidate()
		})
	}()
}

func diskRowErrorText(d *Desktop, row *diskManagerDiskRow) string {
	if row.errText.key != "" {
		return row.errText.localized(d)
	}
	return row.err
}

func diskRowProtectionText(d *Desktop, row *diskManagerDiskRow) string {
	if row.protectedText.key != "" {
		return row.protectedText.localized(d)
	}
	return d.tr(row.protected)
}

func (a *DiskManagerApp) selectDisk(index int) {
	if index < 0 || index >= len(a.rows) || a.busy {
		return
	}
	a.diskSel, a.partSel = index, -1
	a.diskChosen, a.partChosen = true, false
	a.diskScroll.Clamp()
	a.partScroll.Pos = 0
	row := &a.rows[index]
	if row.err != "" {
		a.status = a.d.trf("磁盘不可用: %s", diskRowErrorText(a.d, row))
	} else if row.protected != "" {
		a.status = diskRowProtectionText(a.d, row) + a.d.tr("；可备份分区表，但不能修改")
	} else {
		a.status = a.d.trf("已明确选择 %s；尚未选择分区", row.info.path)
	}
	a.d.Invalidate()
}

func (a *DiskManagerApp) selectPartition(index int) {
	row := a.selectedDisk()
	if row == nil || index < 0 || index >= len(row.device.Partitions) || a.busy {
		return
	}
	a.partSel, a.partChosen = index, true
	a.partScroll.Clamp()
	a.status = a.d.trf("已明确选择分区 %d", row.device.Partitions[index].Number)
	a.d.Invalidate()
}

func (a *DiskManagerApp) targetForDisk(row *diskManagerDiskRow) diskOperationTarget {
	return diskOperationTarget{path: row.info.path, device: *row.device, system: row.info.installer}
}

func (a *DiskManagerApp) targetForPartition(row *diskManagerDiskRow, p *disk.Partition) diskOperationTarget {
	target := a.targetForDisk(row)
	target.partition = p
	return target
}

func (a *DiskManagerApp) deleteSelected() {
	row := a.selectedDisk()
	p := a.selectedPartition(row)
	if !a.canModify() || p == nil {
		return
	}
	partitionPath, _ := diskPartitionDevicePath(row.info.path, p.Number)
	detail := a.d.trf("删除分区 #%d（%s，%s）。", p.Number, partitionPath, diskSizeTextForUI(a.d, p.Bytes()))
	a.requestOperation(diskManagerDelete, a.targetForPartition(row, p), detail, a.startOperation)
}

func (a *DiskManagerApp) resizeSelected() {
	row := a.selectedDisk()
	p := a.selectedPartition(row)
	if !a.canModify() || p == nil {
		return
	}
	currentMiB := p.Bytes() / (1 << 20)
	a.d.InputBoxChecked(a.d.tr("调整分区容量"), a.d.trf("设备 %s，分区 #%d。\n输入新的分区容量（MiB，1 MiB = 2048 扇区）：", row.info.path, p.Number), strconv.FormatUint(currentMiB, 10), func(s string) string {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		if err != nil || n == 0 || n > ^uint64(0)/disk.AlignmentSectors {
			return a.d.tr("请输入有效的正整数容量（MiB）。")
		}
		return ""
	}, func(size string, ok bool) {
		if !ok {
			return
		}
		mib, _ := strconv.ParseUint(strings.TrimSpace(size), 10, 64)
		sectors := mib * disk.AlignmentSectors
		if sectors-1 > ^uint64(0)-p.StartSector {
			a.d.MessageBox(a.d.tr("调整分区"), a.d.tr("容量导致扇区地址溢出。"), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		spec := disk.PartitionSpec{StartSector: p.StartSector, EndSector: p.StartSector + sectors - 1, SizeSectors: sectors}
		if err := validateDesktopPartitionRange(row.device, spec, p.Number); err != nil {
			a.d.MessageBox(a.d.tr("调整分区"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		target := a.targetForPartition(row, p)
		target.spec = spec
		sectorSize := p.SectorSize
		if sectorSize == 0 {
			sectorSize = disk.SectorSize
		}
		detail := a.d.trf("把分区 #%d 从 %s 调整为 %s。\n文件系统不会自动缩放。", p.Number, diskSizeTextForUI(a.d, p.Bytes()), diskSizeTextForUI(a.d, spec.SizeSectors*uint64(sectorSize)))
		a.requestOperation(diskManagerResize, target, detail, a.startOperation)
	})
}

func (a *DiskManagerApp) formatSelected() {
	row := a.selectedDisk()
	p := a.selectedPartition(row)
	if !a.canModify() || p == nil {
		return
	}
	partitionPath, _ := diskPartitionDevicePath(row.info.path, p.Number)
	prompt := a.d.trf("设备 %s\n分区 #%d：%s\n容量：%s\n\n选择文件系统：", row.info.path, p.Number, partitionPath, diskSizeTextForUI(a.d, p.Bytes()))
	a.d.MessageBox(a.d.tr("格式化分区"), prompt, IconQuestion, []string{"ext4", "FAT", a.d.tr("取消")}, func(i int) {
		if i < 0 || i > 1 {
			return
		}
		fs := "ext4"
		if i == 1 {
			fs = "fat"
		}
		target := a.targetForPartition(row, p)
		target.filesystem = fs
		detail := a.d.trf("把分区 #%d（%s，%s）格式化为 %s。", p.Number, partitionPath, diskSizeTextForUI(a.d, p.Bytes()), fs)
		a.requestOperation(diskManagerFormat, target, detail, a.startOperation)
	})
}

func (a *DiskManagerApp) backupSelected() {
	row := a.selectedDisk()
	if !a.canBackup() {
		return
	}
	name := filepath.Base(row.info.path) + "-partition-table.json"
	a.d.FileDialog(FileSave, "/root", name, func(path string, ok bool) {
		if !ok {
			return
		}
		target := a.targetForDisk(row)
		target.backupPath = path
		detail := a.d.trf("把 %s 的 %s 分区表备份到：\n%s", row.info.path, strings.ToUpper(row.device.Table), path)
		a.requestOperation(diskManagerBackup, target, detail, a.startOperation)
	})
}

func readDiskTableBackup(path string) (disk.Backup, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return disk.Backup{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return disk.Backup{}, errors.New("备份必须是普通文件，不能是符号链接")
	}
	if info.Size() > maxDiskTableBackupBytes {
		return disk.Backup{}, errors.New("分区表备份文件过大")
	}
	f, err := os.Open(path)
	if err != nil {
		return disk.Backup{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return disk.Backup{}, errors.New("备份文件在打开时已变化")
	}
	var backup disk.Backup
	dec := json.NewDecoder(io.LimitReader(f, maxDiskTableBackupBytes+1))
	if err := dec.Decode(&backup); err != nil {
		return disk.Backup{}, fmt.Errorf("读取分区表备份: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return disk.Backup{}, errors.New("分区表备份包含多余的 JSON 内容")
	}
	return backup, nil
}

func (a *DiskManagerApp) restoreSelected() {
	row := a.selectedDisk()
	if !a.canModify() {
		return
	}
	a.d.FileDialog(FileOpen, "/root", "", func(path string, ok bool) {
		if !ok {
			return
		}
		backup, err := readDiskTableBackup(path)
		if err != nil {
			a.d.MessageBox(a.d.tr("恢复分区表"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		if backup.Device.SectorSize != row.device.SectorSize || backup.Device.TotalSectors != row.device.TotalSectors || backup.Device.Size != row.device.Size {
			a.d.MessageBox(a.d.tr("恢复分区表"), a.d.tr("备份与所选设备的容量或逻辑扇区不匹配。"), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		target := a.targetForDisk(row)
		target.backup = backup
		target.backupPath = path
		detail := a.d.trf("从备份 %s 恢复分区表。\n备份来源：%s\n备份表：%s", path, backup.SourcePath, strings.ToUpper(backup.Device.Table))
		a.requestOperation(diskManagerRestore, target, detail, a.startOperation)
	})
}

func suggestedDiskPartition(dev *disk.Device) (uint64, uint64) {
	first, last := uint64(disk.AlignmentSectors), dev.TotalSectors-1
	if dev.Table == disk.TableGPT && dev.GPT != nil {
		first, last = dev.GPT.FirstUsableLBA, dev.GPT.LastUsableLBA
	}
	first = alignUpDiskSector(first)
	last = (last + 1) / disk.AlignmentSectors * disk.AlignmentSectors
	if last > 0 {
		last--
	}
	partitions := append([]disk.Partition(nil), dev.Partitions...)
	for i := 1; i < len(partitions); i++ {
		for j := i; j > 0 && partitions[j].StartSector < partitions[j-1].StartSector; j-- {
			partitions[j], partitions[j-1] = partitions[j-1], partitions[j]
		}
	}
	start := first
	for _, p := range partitions {
		if p.EndSector < start {
			continue
		}
		if p.StartSector > start {
			break
		}
		start = alignUpDiskSector(p.EndSector + 1)
	}
	if start > last {
		return 0, 0
	}
	return start, (last - start + 1) / disk.AlignmentSectors
}

func alignUpDiskSector(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	rem := n % disk.AlignmentSectors
	if rem == 0 {
		return n
	}
	if n > ^uint64(0)-(disk.AlignmentSectors-rem) {
		return ^uint64(0)
	}
	return n + disk.AlignmentSectors - rem
}

func validateDesktopPartitionSpec(dev *disk.Device, spec disk.PartitionSpec) error {
	if err := validateDesktopPartitionRange(dev, spec, 0); err != nil {
		return err
	}
	if dev.Table == disk.TableMBR {
		if spec.Name != "" || spec.TypeGUID != "" || spec.Attributes != 0 {
			return errors.New("MBR 不支持 GPT 名称、类型 GUID 或属性")
		}
	} else if spec.Type != 0 || spec.TypeGUID == "" {
		return errors.New("GPT 分区需要有效的类型 GUID")
	}
	return nil
}

// validateDesktopPartitionRange also supports resizing: ignore identifies the
// selected partition, whose own old range must not count as an overlap.
func validateDesktopPartitionRange(dev *disk.Device, spec disk.PartitionSpec, ignore int) error {
	first, last := uint64(disk.AlignmentSectors), dev.TotalSectors-1
	if dev.Table == disk.TableGPT && dev.GPT != nil {
		first, last = dev.GPT.FirstUsableLBA, dev.GPT.LastUsableLBA
	}
	if spec.StartSector < first || spec.EndSector > last || spec.EndSector < spec.StartSector {
		return fmt.Errorf("分区范围 %d-%d 超出可用扇区 %d-%d", spec.StartSector, spec.EndSector, first, last)
	}
	if spec.StartSector%disk.AlignmentSectors != 0 || spec.EndSector == ^uint64(0) || (spec.EndSector+1)%disk.AlignmentSectors != 0 {
		return fmt.Errorf("分区边界必须按 %d 扇区（1 MiB）对齐", disk.AlignmentSectors)
	}
	if spec.SizeSectors != spec.EndSector-spec.StartSector+1 {
		return errors.New("分区容量与结束扇区不一致")
	}
	for _, p := range dev.Partitions {
		if p.Number != ignore && spec.StartSector <= p.EndSector && p.StartSector <= spec.EndSector {
			return fmt.Errorf("与现有分区 #%d 重叠", p.Number)
		}
	}
	if dev.Table == disk.TableMBR && (spec.StartSector > 0xffffffff || spec.EndSector > 0xffffffff || spec.SizeSectors > 0xffffffff) {
		return errors.New("分区超出 MBR 的 32 位 LBA 限制")
	}
	return nil
}

func (a *DiskManagerApp) createSelected() {
	row := a.selectedDisk()
	if !a.canModify() {
		return
	}
	start, maxMiB := suggestedDiskPartition(row.device)
	if start == 0 || maxMiB == 0 {
		a.d.MessageBox(a.d.tr("创建分区"), a.d.tr("没有足够的对齐可用空间。"), IconWarning, []string{a.d.tr("确定")}, nil)
		return
	}
	a.d.InputBoxChecked(a.d.tr("创建分区"), a.d.trf("设备 %s（%s）\n输入起始 LBA（必须按 2048 扇区对齐）：", row.info.path, diskSizeTextForUI(a.d, row.device.Size)), strconv.FormatUint(start, 10), func(s string) string {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		if err != nil || n%disk.AlignmentSectors != 0 {
			return a.d.tr("起始 LBA 必须是 2048 的正整数倍。")
		}
		return ""
	}, func(startText string, ok bool) {
		if !ok {
			return
		}
		start, _ := strconv.ParseUint(strings.TrimSpace(startText), 10, 64)
		a.d.InputBoxChecked(a.d.tr("创建分区"), a.d.tr("输入分区容量（MiB）："), strconv.FormatUint(maxMiB, 10), func(s string) string {
			n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
			if err != nil || n == 0 || n > ^uint64(0)/disk.AlignmentSectors {
				return a.d.tr("请输入有效的正整数容量（MiB）。")
			}
			return ""
		}, func(sizeText string, ok bool) {
			if !ok {
				return
			}
			mib, _ := strconv.ParseUint(strings.TrimSpace(sizeText), 10, 64)
			spec := disk.PartitionSpec{StartSector: start, SizeSectors: mib * disk.AlignmentSectors, TypeGUID: disk.TypeLinuxData}
			if spec.SizeSectors-1 > ^uint64(0)-start {
				a.d.MessageBox(a.d.tr("创建分区"), a.d.tr("容量导致扇区地址溢出。"), IconError, []string{a.d.tr("确定")}, nil)
				return
			}
			spec.EndSector = start + spec.SizeSectors - 1
			if row.device.Table == disk.TableMBR {
				spec.TypeGUID = ""
				spec.Type = 0x83
				a.confirmCreate(row, spec)
				return
			}
			a.d.InputBoxChecked(a.d.tr("创建分区"), a.d.tr("输入 GPT 分区名称（最多 36 个 UTF-16 单元）："), "Linux data", func(s string) string {
				if len(utf16.Encode([]rune(s))) > 36 {
					return a.d.tr("GPT 分区名称过长。")
				}
				return ""
			}, func(name string, ok bool) {
				if !ok {
					return
				}
				spec.Name = strings.TrimSpace(name)
				a.d.MessageBox(a.d.tr("GPT 分区类型"), a.d.trf("设备 %s\n选择新分区的类型：", row.info.path), IconQuestion, []string{a.d.tr("Linux 数据"), a.d.tr("EFI 系统"), a.d.tr("取消")}, func(i int) {
					if i == 0 {
						a.confirmCreate(row, spec)
					} else if i == 1 {
						spec.TypeGUID = disk.TypeESP
						a.confirmCreate(row, spec)
					}
				})
			})
		})
	})
}

func (a *DiskManagerApp) confirmCreate(row *diskManagerDiskRow, spec disk.PartitionSpec) {
	if err := validateDesktopPartitionSpec(row.device, spec); err != nil {
		a.d.MessageBox(a.d.tr("创建分区"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
		return
	}
	target := a.targetForDisk(row)
	target.spec = spec
	slot := strconv.Itoa(spec.Number)
	if spec.Number == 0 {
		slot = a.d.tr("自动分配")
	}
	detail := a.d.trf("创建分区 %s，扇区 %d-%d，容量 %s。", slot, spec.StartSector, spec.EndSector, diskSizeTextForUI(a.d, spec.SizeSectors*uint64(row.device.SectorSize)))
	a.requestOperation(diskManagerCreate, target, detail, a.startOperation)
}

func (a *DiskManagerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	c.FillRect(ui.Rect{r.X + 6, r.Y + 5, r.W - 12, 29}, ui.ColYellow)
	f.TextBold(c, r.X+14, r.Y+13, d.tr("危险：系统盘/数据盘已锁定；仅可修改明确选择的未挂载磁盘。所有修改均需两次确认。"), ui.ColRed)

	diskList := ui.Rect{r.X + 8, r.Y + 48, r.W - 16, 108}
	partList := ui.Rect{r.X + 8, diskList.Bottom() + 25, r.W - 16, r.Bottom() - (diskList.Bottom() + 25) - 108}
	if partList.H < 70 {
		partList.H = 70
	}
	a.diskListR, a.partListR = diskList, partList
	f.TextBold(c, diskList.X, diskList.Y-17, d.tr("磁盘列表（默认未选择）"), ui.ColText)
	f.TextBold(c, partList.X, partList.Y-17, d.tr("分区列表"), ui.ColText)
	a.drawDiskList(c, diskList)
	a.drawPartitionList(c, partList)

	detailsY := partList.Bottom() + 7
	if row := a.selectedDisk(); row != nil {
		state := d.tr("未挂载，可编辑")
		if row.protected != "" {
			state = diskRowProtectionText(d, row)
		}
		f.TextClip(c, r.X+10, detailsY, r.W-20, d.trf("设备: %s  容量: %s  扇区: %d  B  总扇区: %d", row.info.path, diskSizeTextForUI(d, row.device.Size), row.device.SectorSize, row.device.TotalSectors), ui.ColText)
		f.TextClip(c, r.X+10, detailsY+18, r.W-20, d.trf("分区表: %s  状态: %s", strings.ToUpper(row.device.Table), state), ui.ColText)
	} else {
		f.Text(c, r.X+10, detailsY, d.tr("未选择磁盘。请单击一块磁盘；系统盘和数据盘不会被默认选中。"), ui.ColShadow)
	}
	if a.err != "" {
		f.TextClip(c, r.X+10, detailsY+36, r.W-20, a.err, ui.ColRed)
	} else {
		f.TextClip(c, r.X+10, detailsY+36, r.W-20, d.tr(a.status), ui.ColShadow)
	}

	buttonY := r.Bottom() - 58
	gap := 8
	width := (r.W - 16 - gap*3) / 4
	rect := func(i int) ui.Rect { return ui.Rect{r.X + 8 + i*(width+gap), buttonY, width, 25} }
	a.refreshR, a.createR, a.deleteR, a.resizeR = rect(0), rect(1), rect(2), rect(3)
	row := a.selectedDisk()
	p := a.selectedPartition(row)
	ui.Button(c, a.refreshR, d.tr("刷新(F5)"), a.press == "refresh", !a.busy)
	ui.Button(c, a.createR, d.tr("创建分区"), a.press == "create", a.canModify())
	ui.Button(c, a.deleteR, d.tr("删除分区"), a.press == "delete", a.canModify() && p != nil)
	ui.Button(c, a.resizeR, d.tr("调整容量"), a.press == "resize", a.canModify() && p != nil)
	buttonY += 31
	a.formatR, a.backupR, a.restoreR, a.closeR = rect(0), rect(1), rect(2), rect(3)
	ui.Button(c, a.formatR, d.tr("格式化"), a.press == "format", a.canModify() && p != nil)
	ui.Button(c, a.backupR, d.tr("备份分区表"), a.press == "backup", a.canBackup())
	ui.Button(c, a.restoreR, d.tr("恢复分区表"), a.press == "restore", a.canModify())
	ui.Button(c, a.closeR, d.tr("关闭"), a.press == "close", !a.busy)
}

func (a *DiskManagerApp) drawDiskList(c *ui.Canvas, list ui.Rect) {
	f := ui.DefaultFont()
	c.FillRect(list, ui.ColField)
	c.Sunken(list)
	inner := list.Inset(2)
	scrollW := 16
	tableW := inner.W - scrollW - 2
	headerH, rowH := 20, 22
	c.FillRect(ui.Rect{inner.X, inner.Y, tableW, headerH}, ui.ColFace)
	pathW, sizeW, tableTypeW := 142, 88, 52
	f.Text(c, inner.X+4, inner.Y+4, a.d.tr("设备"), ui.ColText)
	f.Text(c, inner.X+pathW, inner.Y+4, a.d.tr("型号"), ui.ColText)
	f.Text(c, inner.X+pathW+tableW-sizeW-tableTypeW-58, inner.Y+4, a.d.tr("容量"), ui.ColText)
	f.Text(c, inner.X+tableW-tableTypeW-58, inner.Y+4, a.d.tr("分区表"), ui.ColText)
	f.Text(c, inner.X+tableW-56, inner.Y+4, a.d.tr("状态"), ui.ColText)
	rows := maxInt(1, (inner.H-headerH-2)/rowH)
	a.diskScroll.Visible = rows
	a.diskScroll.Clamp()
	a.diskRowRects = a.diskRowRects[:0]
	y := inner.Y + headerH + 1
	for i := a.diskScroll.Pos; i < len(a.rows) && y+rowH <= inner.Bottom(); i++ {
		row := a.rows[i]
		rr := ui.Rect{inner.X + 1, y, tableW - 2, rowH - 1}
		a.diskRowRects = append(a.diskRowRects, rr)
		col := ui.ColText
		if i == a.diskSel && a.diskChosen {
			c.FillRect(rr, ui.ColNavy)
			col = ui.ColWhite
		}
		ui.DrawIcon16(c, ui.IcoDrive, rr.X+3, rr.Y+2)
		f.TextClip(c, rr.X+23, rr.Y+4, pathW-27, row.info.path, col)
		model := row.info.model
		if model == "" {
			model = a.d.tr("虚拟磁盘")
		}
		modelW := tableW - pathW - sizeW - tableTypeW - 70
		f.TextClip(c, rr.X+pathW, rr.Y+4, maxInt(30, modelW), model, col)
		capacity := a.d.tr("读取失败")
		tableType, state := "-", diskRowErrorText(a.d, &row)
		if row.device != nil {
			capacity = diskSizeTextForUI(a.d, row.device.Size)
			tableType = strings.ToUpper(row.device.Table)
			state = a.d.trf("%d 分区", len(row.device.Partitions))
			if row.protected != "" {
				state = a.d.tr("受保护")
			}
		}
		f.TextClip(c, rr.Right()-sizeW-tableTypeW-56, rr.Y+4, sizeW, capacity, col)
		f.TextClip(c, rr.Right()-tableTypeW-56, rr.Y+4, tableTypeW, tableType, col)
		stateCol := col
		if row.protected != "" && col != ui.ColWhite {
			stateCol = ui.ColRed
		}
		f.TextClip(c, rr.Right()-54, rr.Y+4, 50, state, stateCol)
		y += rowH
	}
	if len(a.rows) == 0 {
		f.Text(c, inner.X+6, inner.Y+headerH+6, a.d.tr("没有可显示的磁盘。"), ui.ColShadow)
	}
	a.diskScrollR = ui.Rect{inner.Right() - scrollW, inner.Y + headerH + 1, scrollW, inner.H - headerH - 2}
	a.diskScroll.Draw(c, a.diskScrollR)
}

func (a *DiskManagerApp) drawPartitionList(c *ui.Canvas, list ui.Rect) {
	f := ui.DefaultFont()
	c.FillRect(list, ui.ColField)
	c.Sunken(list)
	inner := list.Inset(2)
	scrollW := 16
	tableW := inner.W - scrollW - 2
	headerH, rowH := 20, 22
	c.FillRect(ui.Rect{inner.X, inner.Y, tableW, headerH}, ui.ColFace)
	numberW, nameW, sizeW, fsW := 34, 116, 88, 78
	f.Text(c, inner.X+4, inner.Y+4, "#", ui.ColText)
	f.Text(c, inner.X+numberW, inner.Y+4, a.d.tr("名称"), ui.ColText)
	f.Text(c, inner.X+numberW+nameW, inner.Y+4, a.d.tr("容量"), ui.ColText)
	f.Text(c, inner.X+numberW+nameW+sizeW, inner.Y+4, a.d.tr("文件系统"), ui.ColText)
	f.Text(c, inner.X+numberW+nameW+sizeW+fsW, inner.Y+4, a.d.tr("起始-结束 LBA / 类型"), ui.ColText)
	row := a.selectedDisk()
	count := 0
	if row != nil {
		count = len(row.device.Partitions)
	}
	rows := maxInt(1, (inner.H-headerH-2)/rowH)
	a.partScroll.Total = count
	a.partScroll.Visible = rows
	a.partScroll.Clamp()
	a.partRowRects = a.partRowRects[:0]
	y := inner.Y + headerH + 1
	if row != nil {
		for i := a.partScroll.Pos; i < count && y+rowH <= inner.Bottom(); i++ {
			p := row.device.Partitions[i]
			rr := ui.Rect{inner.X + 1, y, tableW - 2, rowH - 1}
			a.partRowRects = append(a.partRowRects, rr)
			col := ui.ColText
			if i == a.partSel && a.partChosen {
				c.FillRect(rr, ui.ColNavy)
				col = ui.ColWhite
			}
			f.Text(c, rr.X+5, rr.Y+4, strconv.Itoa(p.Number), col)
			name := p.Name
			if name == "" {
				name = a.d.trf("分区 %d", p.Number)
			}
			f.TextClip(c, rr.X+numberW, rr.Y+4, nameW-6, name, col)
			f.TextClip(c, rr.X+numberW+nameW, rr.Y+4, sizeW, diskSizeTextForUI(a.d, p.Bytes()), col)
			f.TextClip(c, rr.X+numberW+nameW+sizeW, rr.Y+4, fsW, a.d.tr(row.filesys[p.Number]), col)
			typeText := strings.ToUpper(p.Table)
			if p.Table == disk.TableMBR {
				typeText += fmt.Sprintf(" 0x%02X", p.Type)
			}
			f.TextClip(c, rr.X+numberW+nameW+sizeW+fsW, rr.Y+4, maxInt(40, tableW-numberW-nameW-sizeW-fsW-8), fmt.Sprintf("%d-%d  %s", p.StartSector, p.EndSector, typeText), col)
			y += rowH
		}
	}
	if count == 0 {
		message := a.d.tr("请先明确选择一块磁盘。")
		if row != nil && len(row.device.Partitions) == 0 {
			message = a.d.tr("该磁盘没有分区。")
		}
		f.Text(c, inner.X+6, inner.Y+headerH+6, message, ui.ColShadow)
	}
	a.partScrollR = ui.Rect{inner.Right() - scrollW, inner.Y + headerH + 1, scrollW, inner.H - headerH - 2}
	a.partScroll.Draw(c, a.partScrollR)
}

func (a *DiskManagerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if changed, consumed := a.diskScroll.Mouse(ev, a.diskScrollR); changed || consumed {
		if ev.Kind == MouseUp {
			a.press = ""
		}
		d.Invalidate()
		return
	}
	if changed, consumed := a.partScroll.Mouse(ev, a.partScrollR); changed || consumed {
		if ev.Kind == MouseUp {
			a.press = ""
		}
		d.Invalidate()
		return
	}
	if ev.Kind == MouseWheel {
		if a.diskListR.Contains(ev.X, ev.Y) {
			a.diskScroll.ScrollBy(-ev.Wheel)
		} else if a.partListR.Contains(ev.X, ev.Y) {
			a.partScroll.ScrollBy(-ev.Wheel)
		}
		d.Invalidate()
		return
	}
	if ev.Button != 1 || a.busy {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.refreshR.Contains(ev.X, ev.Y):
			a.press = "refresh"
		case a.createR.Contains(ev.X, ev.Y) && a.canModify():
			a.press = "create"
		case a.deleteR.Contains(ev.X, ev.Y) && a.canModify() && a.selectedPartition(a.selectedDisk()) != nil:
			a.press = "delete"
		case a.resizeR.Contains(ev.X, ev.Y) && a.canModify() && a.selectedPartition(a.selectedDisk()) != nil:
			a.press = "resize"
		case a.formatR.Contains(ev.X, ev.Y) && a.canModify() && a.selectedPartition(a.selectedDisk()) != nil:
			a.press = "format"
		case a.backupR.Contains(ev.X, ev.Y) && a.canBackup():
			a.press = "backup"
		case a.restoreR.Contains(ev.X, ev.Y) && a.canModify():
			a.press = "restore"
		case a.closeR.Contains(ev.X, ev.Y):
			a.press = "close"
		default:
			for i, rr := range a.diskRowRects {
				if rr.Contains(ev.X, ev.Y) {
					a.selectDisk(a.diskScroll.Pos + i)
					a.press = "disk"
					break
				}
			}
			if a.press == "" {
				for i, rr := range a.partRowRects {
					if rr.Contains(ev.X, ev.Y) {
						a.selectPartition(a.partScroll.Pos + i)
						a.press = "part"
						break
					}
				}
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "refresh":
			if a.refreshR.Contains(ev.X, ev.Y) {
				a.refresh()
			}
		case "create":
			if a.createR.Contains(ev.X, ev.Y) {
				a.createSelected()
			}
		case "delete":
			if a.deleteR.Contains(ev.X, ev.Y) {
				a.deleteSelected()
			}
		case "resize":
			if a.resizeR.Contains(ev.X, ev.Y) {
				a.resizeSelected()
			}
		case "format":
			if a.formatR.Contains(ev.X, ev.Y) {
				a.formatSelected()
			}
		case "backup":
			if a.backupR.Contains(ev.X, ev.Y) {
				a.backupSelected()
			}
		case "restore":
			if a.restoreR.Contains(ev.X, ev.Y) {
				a.restoreSelected()
			}
		case "close":
			if a.closeR.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
	}
	d.Invalidate()
}

func (a *DiskManagerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F5":
		if !a.busy {
			a.refresh()
		}
		return true
	case "Esc":
		if !a.busy {
			d.CloseWindow(w)
		}
		return true
	}
	return false
}
