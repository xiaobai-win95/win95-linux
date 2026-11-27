package desktop

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"win95linux/internal/disk"
	"win95linux/internal/ui"
)

func TestDiskManagerRequiresExplicitUnprotectedSelection(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	dev := &disk.Device{
		Path: "/dev/vdb", Size: 64 << 30, SectorSize: disk.SectorSize,
		TotalSectors: (64 << 30) / disk.SectorSize, Table: disk.TableGPT,
		GPT:        &disk.GPT{FirstUsableLBA: 2048, LastUsableLBA: (64<<30)/disk.SectorSize - 34},
		Partitions: []disk.Partition{{Number: 1, Table: disk.TableGPT, StartSector: 2048, EndSector: 6143, SectorSize: disk.SectorSize}},
	}
	a := &DiskManagerApp{d: d, diskSel: -1, partSel: -1, rows: []diskManagerDiskRow{{info: diskInfo{path: dev.Path, sizeBytes: int64(dev.Size)}, device: dev}}}
	w := d.Open(a)
	a.Paint(d, w, ui.NewCanvas(1024, 768), w.ClientRect())
	if a.canModify() || a.canBackup() || a.selectedDisk() != nil {
		t.Fatal("a disk was selected by default")
	}
	a.selectDisk(0)
	if !a.canModify() || a.selectedPartition(a.selectedDisk()) != nil {
		t.Fatal("explicit disk selection did not unlock only disk-level actions")
	}
	a.rows[0].protected = "受保护：系统/数据盘已挂载于 /data"
	if a.canModify() || !a.canBackup() {
		t.Fatal("protected disk must not be editable, but may be backed up")
	}
}

func TestDiskMountParsingAndFailClosedProtection(t *testing.T) {
	data := []byte("36 25 8:1 / / rw,relatime - ext4 /dev/vda1 rw\n" +
		"40 25 8:17 / /data rw,relatime - xfs /dev/vdb1 rw\n")
	mounts, err := parseDiskMountInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].Source != "/dev/vda1" || mounts[1].Target != "/data" || mounts[1].FS != "xfs" {
		t.Fatalf("unexpected mounts: %+v", mounts)
	}
	if mounts[0].DevID != "8:1" || mounts[0].Major != 8 || mounts[0].Minor != 1 {
		t.Fatalf("device identity was not parsed: %+v", mounts[0])
	}
	owners := map[string][]diskDeviceRef{
		"8:1":  {{DevID: "8:0", Name: "vda"}},
		"8:17": {{DevID: "8:16", Name: "vdb"}},
	}
	ownerFn := func(id string) []diskDeviceRef { return owners[id] }
	dev := &disk.Device{Path: "/dev/vdb"}
	if protected, err := diskSafetyStatus(dev.Path, dev, mounts, ownerFn, false); err != nil || !strings.Contains(protected, "/data") {
		t.Fatalf("protected = %q, err = %v", protected, err)
	}
	if protected, err := diskSafetyStatus("/dev/vdc", dev, mounts, ownerFn, false); err != nil || protected != "" {
		t.Fatalf("unmounted disk protected = %q, err = %v", protected, err)
	}
	unknown := []diskMountInfo{{Source: "/dev/mapper/unknown", Target: "/srv", FS: "ext4", DevID: "9:0", Major: 9}}
	if _, err := diskSafetyStatus("/dev/vdc", dev, unknown, ownerFn, false); err == nil {
		t.Fatal("unknown mounted block device must fail closed")
	}
	if protected, err := diskSafetyStatus("/dev/vdc", dev, nil, ownerFn, true); err != nil || protected == "" {
		t.Fatalf("installer disk protected = %q, err = %v", protected, err)
	}
	if !diskSourceMatchesPath("/dev/nvme0n1p2", "/dev/nvme0n1") || diskSourceMatchesPath("/dev/nvme0n10", "/dev/nvme0n1") {
		t.Fatal("partition path ownership matching is incorrect")
	}
}

func TestDiskSafetyMatchesMajorMinorAcrossAliases(t *testing.T) {
	owners := map[string][]diskDeviceRef{
		"253:1": {{DevID: "253:0", Name: "dm-0"}},
		"8:2":   {{DevID: "8:0", Name: "vda"}},
	}
	ownerFn := func(id string) []diskDeviceRef { return owners[id] }

	// /dev/root and /dev/mapper/system have different names, but the mounted
	// partition and selected disk are related by their kernel device identities.
	dev := &disk.Device{Path: "/dev/mapper/system", DeviceID: "253:0"}
	mounts := []diskMountInfo{{Source: "/dev/root", Target: "/", FS: "ext4", DevID: "253:1", Major: 253, Minor: 1}}
	if protected, err := diskSafetyStatus(dev.Path, dev, mounts, ownerFn, false); err != nil || !strings.Contains(protected, "/") {
		t.Fatalf("aliased system disk protected = %q, err = %v", protected, err)
	}

	// A mounted partition alias must also match when the partition itself is
	// selected, without relying on its path or sysfs owner name.
	dev = &disk.Device{Path: "/dev/mapper/data", DeviceID: "253:1"}
	if protected, err := diskSafetyStatus(dev.Path, dev, mounts, ownerFn, false); err != nil || protected == "" {
		t.Fatalf("aliased partition protected = %q, err = %v", protected, err)
	}

	// Equal majors with different minors are different devices.
	dev = &disk.Device{Path: "/dev/vda1", DeviceID: "8:1"}
	otherMount := []diskMountInfo{{Source: "/dev/vda2", Target: "/data", FS: "xfs", DevID: "8:2", Major: 8, Minor: 2}}
	if protected, err := diskSafetyStatus(dev.Path, dev, otherMount, ownerFn, false); err != nil || protected != "" {
		t.Fatalf("different minor protected = %q, err = %v", protected, err)
	}
	if _, err := parseDiskMountInfo([]byte("36 25 8:1:2 / / rw - ext4 /dev/root rw\n")); err == nil {
		t.Fatal("malformed major:minor identity accepted")
	}
}

func TestDiskGeometryAndPartitionGuards(t *testing.T) {
	want := &disk.Device{Size: 64 << 30, SectorSize: 512, TotalSectors: 1 << 26, Table: disk.TableGPT}
	got := *want
	if !sameDiskGeometry(want, &got) {
		t.Fatal("identical geometry rejected")
	}
	got.TotalSectors--
	if sameDiskGeometry(want, &got) {
		t.Fatal("changed geometry accepted")
	}
	got = *want
	got.DeviceID = "8:16"
	if sameDiskGeometry(want, &got) {
		t.Fatal("replaced device with identical geometry accepted")
	}
	p := disk.Partition{Number: 2, Table: disk.TableGPT, StartSector: 2048, EndSector: 4095, SectorSize: 512}
	p2 := p
	if !sameDiskPartition(&p, &p2) {
		t.Fatal("identical partition rejected")
	}
	p2.EndSector--
	if sameDiskPartition(&p, &p2) {
		t.Fatal("changed partition accepted")
	}
}

func TestDiskOperationNeedsTwoWarnings(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	target := diskOperationTarget{path: "/dev/vdc", device: disk.Device{Size: 32 << 30, Table: disk.TableGPT}}
	proceeded, cancelled := 0, 0
	proceed := func() { proceeded++ }
	cancel := func() { cancelled++ }
	confirmDiskOperation(d, diskManagerDelete, target, "删除分区 #2，容量 1.0 GB。", proceed, cancel)
	first := d.modal
	if first == nil || !strings.Contains(strings.Join(first.App.(*dialogApp).lines, "\n"), target.path) {
		t.Fatal("first warning does not identify the exact device")
	}
	clickDesktopDialogButton(t, d, 0)
	if d.modal == nil || d.modal == first || !strings.Contains(d.modal.App.Title(), "最后确认") {
		t.Fatal("second warning was not shown")
	}
	clickDesktopDialogButton(t, d, 1)
	if proceeded != 0 || cancelled != 1 {
		t.Fatalf("cancel flow proceeded=%d cancelled=%d", proceeded, cancelled)
	}
	confirmDiskOperation(d, diskManagerDelete, target, "删除分区 #2，容量 1.0 GB。", proceed, cancel)
	clickDesktopDialogButton(t, d, 0)
	clickDesktopDialogButton(t, d, 0)
	if proceeded != 1 {
		t.Fatal("operation ran without the second confirmation")
	}
}

func TestDiskFilesystemDetectionAndPartitionValidation(t *testing.T) {
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext4.img")
	data := make([]byte, 2048)
	binary.LittleEndian.PutUint16(data[1024+56:], 0xef53)
	binary.LittleEndian.PutUint32(data[1024+96:], 0x40)
	if err := os.WriteFile(ext, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectDiskFilesystem(ext); got != "ext4" {
		t.Fatalf("filesystem = %q, want ext4", got)
	}
	fat := filepath.Join(dir, "fat.img")
	data = make([]byte, 512)
	copy(data[82:90], "FAT32   ")
	data[510], data[511] = 0x55, 0xaa
	if err := os.WriteFile(fat, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectDiskFilesystem(fat); got != "FAT32" {
		t.Fatalf("filesystem = %q, want FAT32", got)
	}
	dev := &disk.Device{Table: disk.TableGPT, TotalSectors: 1 << 20, GPT: &disk.GPT{FirstUsableLBA: 2048, LastUsableLBA: (1 << 20) - 34}, Partitions: []disk.Partition{{Number: 1, StartSector: 2048, EndSector: 4095}}}
	valid := disk.PartitionSpec{StartSector: 6144, EndSector: 8191, SizeSectors: 2048, TypeGUID: disk.TypeLinuxData}
	if err := validateDesktopPartitionSpec(dev, valid); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}
	valid.StartSector = 3000
	if err := validateDesktopPartitionSpec(dev, valid); err == nil {
		t.Fatal("unaligned partition accepted")
	}
	resize := disk.PartitionSpec{StartSector: 2048, EndSector: 6143, SizeSectors: 4096}
	if err := validateDesktopPartitionRange(dev, resize, 1); err != nil {
		t.Fatalf("resizing the selected partition was rejected: %v", err)
	}
	if err := validateDesktopPartitionRange(dev, resize, 0); err == nil {
		t.Fatal("new partition overlapping an existing partition accepted")
	}
}

func TestDiskBackupReaderRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "backup.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readDiskTableBackup(link); err == nil {
		t.Fatal("symlink backup accepted")
	}
}

func clickDesktopDialogButton(t *testing.T, d *Desktop, index int) {
	t.Helper()
	d.Draw(ui.NewCanvas(d.W, d.H))
	if d.modal == nil {
		t.Fatal("expected a modal dialog")
	}
	app, ok := d.modal.App.(*dialogApp)
	if !ok || index < 0 || index >= len(app.rects) {
		t.Fatalf("dialog button %d unavailable", index)
	}
	r := app.rects[index]
	x, y := r.X+r.W/2, r.Y+r.H/2
	d.Mouse(MouseEvent{Kind: MouseDown, X: x, Y: y, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: x, Y: y, Button: 1})
}

func runDesktopPostedUntil(t *testing.T, d *Desktop, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ready() {
		d.RunPosted()
		if ready() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for posted desktop callback")
		}
		runtime.Gosched()
	}
	d.RunPosted()
}

func TestDiskManagerWorkerPostsCompletion(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	target := diskOperationTarget{path: "/dev/vdc", device: disk.Device{Size: 8 << 30, Table: disk.TableMBR}}
	calls := 0
	a := &DiskManagerApp{
		d:         d,
		listFn:    func() ([]diskInfo, error) { return nil, nil },
		mountsFn:  func() ([]diskMountInfo, error) { return nil, nil },
		prepareFn: func(got diskOperationTarget) (diskOperationTarget, error) { return got, nil },
		runFn: func(op diskManagerOp, got diskOperationTarget) error {
			calls++
			return nil
		},
	}
	a.startOperation(diskManagerBackup, target)
	runDesktopPostedUntil(t, d, func() bool { return d.modal != nil && !a.busy })
	if calls != 1 {
		t.Fatalf("calls=%d status=%q", calls, a.status)
	}
	if !strings.Contains(d.modal.App.Title(), "完成") {
		t.Fatal("completion dialog was not posted to the UI")
	}
}

func TestDiskRequestOperationRunsOnlyAfterTwoConfirmations(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	// A malformed version-zero backup must not downgrade restore to backup and
	// bypass destructive-operation mount checks.
	target := diskOperationTarget{path: "/dev/vdc", device: disk.Device{Size: 8 << 30, Table: disk.TableMBR}, backupPath: "/root/table.json"}
	var calls atomic.Int32
	a := &DiskManagerApp{
		d:         d,
		listFn:    func() ([]diskInfo, error) { return nil, nil },
		mountsFn:  func() ([]diskMountInfo, error) { return nil, nil },
		prepareFn: func(got diskOperationTarget) (diskOperationTarget, error) { return got, nil },
		runFn: func(op diskManagerOp, got diskOperationTarget) error {
			if op != diskManagerRestore || got.op() != diskManagerRestore {
				t.Errorf("restore operation was misclassified: op=%v target=%v", op, got.op())
			}
			calls.Add(1)
			return nil
		},
	}
	a.requestOperation(diskManagerRestore, target, "恢复分区表。", a.startOperation)
	runDesktopPostedUntil(t, d, func() bool { return d.modal != nil })
	if calls.Load() != 0 {
		t.Fatal("operation ran before confirmation")
	}
	clickDesktopDialogButton(t, d, 0)
	if calls.Load() != 0 || d.modal == nil || !strings.Contains(d.modal.App.Title(), "最后确认") {
		t.Fatal("operation ran after only one confirmation")
	}
	clickDesktopDialogButton(t, d, 0)
	runDesktopPostedUntil(t, d, func() bool {
		return calls.Load() == 1 && d.modal != nil && !a.busy && strings.Contains(d.modal.App.Title(), "完成")
	})
}

func TestSuggestedDiskPartitionStaysAligned(t *testing.T) {
	dev := &disk.Device{Table: disk.TableGPT, TotalSectors: 1 << 20, GPT: &disk.GPT{FirstUsableLBA: 2048, LastUsableLBA: (1 << 20) - 34}, Partitions: []disk.Partition{{Number: 1, StartSector: 2048, EndSector: 4095}}}
	start, mib := suggestedDiskPartition(dev)
	if start != 4096 || mib == 0 || start%disk.AlignmentSectors != 0 {
		t.Fatalf("start=%d mib=%d", start, mib)
	}
}
