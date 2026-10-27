//go:build linux

package desktop

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/gpt"
)

const (
	payloadDir      = "/payload"
	payloadRootfs   = payloadDir + "/rootfs.tar.gz"
	installerLabel  = "WIN95INST"
	targetESPLabel  = "WIN95ESP"
	targetRootLabel = "WIN95ROOT"
	minDiskBytes    = int64(1 << 30)
	mib             = uint64(1 << 20)
	espSizeBytes    = 256 * mib
	alignSectors    = uint64(2048)
)

func readSys(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func listDisks() ([]diskInfo, error) {
	var out []diskInfo
	for _, dev := range gpt.BlockDevices() {
		base := filepath.Base(dev)
		sys := "/sys/block/" + base
		sectors, err := strconv.ParseInt(readSys(sys+"/size"), 10, 64)
		if err != nil || sectors <= 0 {
			continue
		}
		size := sectors * 512
		if size < 32<<20 {
			continue
		}
		model := readSys(sys + "/device/model")
		if model == "" {
			model = readSys(sys + "/device/vendor")
		}
		removable := readSys(sys+"/removable") == "1"
		d := diskInfo{
			path:      dev,
			model:     strings.TrimSpace(model),
			sizeBytes: size,
			removable: removable,
		}
		if t, err := gpt.Read(dev); err == nil {
			d.hasTable = true
			d.parts = len(t.Partitions)
			if _, ok := gpt.FindByLabel(t, installerLabel); ok {
				d.installer = true
			}
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, errors.New("未找到磁盘设备。请确认虚拟机已添加 VirtIO 磁盘。")
	}
	return out, nil
}

func runTool(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"PATH=/sbin:/bin:/usr/sbin:/usr/bin",
		"LD_LIBRARY_PATH=/lib:/usr/lib",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 1200 {
			msg = msg[len(msg)-1200:]
		}
		return fmt.Errorf("%s 失败: %v\n%s", filepath.Base(name), err, msg)
	}
	return nil
}

func installSystem(target diskInfo, progress func(installProgress)) (installResult, error) {
	var result installResult
	report := func(step, pct int, msg string) {
		if progress != nil {
			progress(installProgress{step: step, pct: pct, msg: msg})
		}
	}
	if target.installer {
		return result, errors.New("不能安装到安装盘本身")
	}
	if target.sizeBytes < minDiskBytes {
		return result, fmt.Errorf("目标磁盘空间不足（需要至少 1GB，当前 %s）", formatBytes(target.sizeBytes))
	}
	if _, err := os.Stat(payloadRootfs); err != nil {
		return result, fmt.Errorf("安装数据缺失: %v", err)
	}

	report(0, -1, "正在准备目标磁盘...")
	for _, dir := range []string{"/mnt/win95-esp", "/mnt/win95-root"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return result, fmt.Errorf("创建挂载点失败: %v", err)
		}
	}
	for i := 1; i <= 16; i++ {
		_ = syscall.Unmount(gpt.PartitionDevice(target.path, i), syscall.MNT_DETACH)
	}
	syscall.Sync()

	f, err := os.OpenFile(target.path, os.O_RDWR, 0)
	if err != nil {
		return result, fmt.Errorf("无法打开目标磁盘: %v", err)
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return result, fmt.Errorf("无法读取目标磁盘大小: %v", err)
	}
	totalSectors := uint64(end) / gpt.SectorSize
	if totalSectors < 4*1024*1024 {
		f.Close()
		return result, errors.New("目标磁盘太小")
	}
	zero := make([]byte, mib)
	if _, err := f.WriteAt(zero, 0); err != nil {
		f.Close()
		return result, fmt.Errorf("清理磁盘失败: %v", err)
	}
	if end > int64(mib) {
		if _, err := f.WriteAt(zero, end-int64(mib)); err != nil {
			f.Close()
			return result, fmt.Errorf("清理磁盘尾部失败: %v", err)
		}
	}
	f.Close()
	syscall.Sync()

	espStart := alignSectors
	espSectors := espSizeBytes / gpt.SectorSize
	rootStart := alignUp(espStart+espSectors, alignSectors)
	rootEnd := totalSectors - 34
	if rootEnd <= rootStart || (rootEnd-rootStart+1)*gpt.SectorSize < 384*mib {
		return result, errors.New("目标磁盘空间不足以容纳系统")
	}
	report(1, -1, "正在写入 GPT 分区表...")
	table, err := gpt.Write(target.path, totalSectors, []gpt.NewPartition{
		{Name: targetESPLabel, TypeGUID: gpt.TypeESP, FirstLBA: espStart, LastLBA: espStart + espSectors - 1},
		{Name: targetRootLabel, TypeGUID: gpt.TypeLinuxData, FirstLBA: rootStart, LastLBA: rootEnd},
	})
	if err != nil {
		return result, fmt.Errorf("写入分区表失败: %v", err)
	}
	syscall.Sync()
	espDev, err := waitPartition(target.path, 1)
	if err != nil {
		return result, err
	}
	rootDev, err := waitPartition(target.path, 2)
	if err != nil {
		return result, err
	}

	report(2, -1, "正在格式化 EFI 系统分区（FAT32）...")
	if err := runTool("/sbin/mkfs.fat", "-F", "32", "-n", targetESPLabel, espDev); err != nil {
		return result, err
	}
	report(3, -1, "正在格式化 Linux 系统分区（ext4）...")
	if err := runTool("/sbin/mke2fs", "-t", "ext4", "-F", "-L", targetRootLabel, "-m", "0", rootDev); err != nil {
		return result, err
	}
	syscall.Sync()

	report(4, 0, "正在挂载目标分区...")
	if err := os.MkdirAll("/mnt/win95-esp", 0o755); err != nil {
		return result, err
	}
	if err := syscall.Mount(espDev, "/mnt/win95-esp", "vfat", 0, "iocharset=utf8,shortname=mixed"); err != nil {
		return result, fmt.Errorf("挂载 EFI 分区失败: %v", err)
	}
	unmounted := false
	defer func() {
		if !unmounted {
			_ = syscall.Unmount("/mnt/win95-root", syscall.MNT_DETACH)
			_ = syscall.Unmount("/mnt/win95-esp", syscall.MNT_DETACH)
		}
	}()
	if err := syscall.Mount(rootDev, "/mnt/win95-root", "ext4", 0, ""); err != nil {
		return result, fmt.Errorf("挂载系统分区失败: %v", err)
	}

	if err := extractRootfs(payloadRootfs, "/mnt/win95-root", func(pct int, msg string) {
		report(4, pct, msg)
	}); err != nil {
		return result, err
	}
	report(4, 100, "系统文件解压完成")

	report(5, -1, "正在复制内核与引导程序...")
	for _, dir := range []string{"/mnt/win95-esp/EFI/BOOT", "/mnt/win95-esp/loader/entries"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return result, err
		}
	}
	if err := copyFile(filepath.Join(payloadDir, "Image"), "/mnt/win95-esp/Image"); err != nil {
		return result, fmt.Errorf("复制内核失败: %v", err)
	}
	if err := copyFile(filepath.Join(payloadDir, "initrd-installed.img"), "/mnt/win95-esp/initrd"); err != nil {
		return result, fmt.Errorf("复制启动镜像失败: %v", err)
	}
	if err := copyFile(filepath.Join(payloadDir, "BOOTAA64.EFI"), "/mnt/win95-esp/EFI/BOOT/BOOTAA64.EFI"); err != nil {
		return result, fmt.Errorf("复制引导程序失败: %v", err)
	}

	rootUUID := strings.ToLower(table.Partitions[1].UniqueGUID)
	result.rootUUID = rootUUID
	entry := "title Windows 95 Linux 中文版\n" +
		"linux /Image\n" +
		"initrd /initrd\n" +
		"options win95.root=PARTUUID=" + rootUUID + " rootwait quiet loglevel=3 console=tty0 consoleblank=0 net.ifnames=0\n"
	if err := os.WriteFile("/mnt/win95-esp/loader/entries/win95.conf", []byte(entry), 0o644); err != nil {
		return result, err
	}
	loaderConf := "default win95\ntimeout 3\neditor no\nauto-entries no\n"
	if err := os.WriteFile("/mnt/win95-esp/loader/loader.conf", []byte(loaderConf), 0o644); err != nil {
		return result, err
	}

	fstab := fmt.Sprintf("PARTUUID=%s / ext4 rw,relatime 0 1\n"+
		"proc /proc proc defaults 0 0\n"+
		"sysfs /sys sysfs defaults 0 0\n"+
		"devtmpfs /dev devtmpfs mode=0755,nosuid 0 0\n"+
		"devpts /dev/pts devpts gid=5,mode=0620 0 0\n"+
		"tmpfs /tmp tmpfs mode=1777,nosuid,nodev 0 0\n"+
		"tmpfs /run tmpfs mode=0755,nosuid,nodev 0 0\n", rootUUID)
	if err := os.MkdirAll("/mnt/win95-root/etc", 0o755); err != nil {
		return result, err
	}
	if err := os.WriteFile("/mnt/win95-root/etc/fstab", []byte(fstab), 0o644); err != nil {
		return result, err
	}

	report(6, 95, "正在同步磁盘数据...")
	syscall.Sync()
	_ = syscall.Unmount("/mnt/win95-root", syscall.MNT_DETACH)
	_ = syscall.Unmount("/mnt/win95-esp", syscall.MNT_DETACH)
	unmounted = true
	syscall.Sync()
	report(6, 100, "安装完成")
	return result, nil
}

func alignUp(v, a uint64) uint64 {
	if a == 0 {
		return v
	}
	return (v + a - 1) / a * a
}

func waitPartition(disk string, index int) (string, error) {
	dev := gpt.PartitionDevice(disk, index)
	base := filepath.Base(dev)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(dev); err == nil {
			return dev, nil
		}
		if node, ok := partitionNodeFromSysfs(base); ok {
			return node, nil
		}
		if time.Now().After(deadline) {
			break
		}
		// Some kernels ignore the rescan issued while the disk was still
		// open for writing, so keep asking rather than trying only once.
		_ = gpt.RereadPartitions(disk)
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("等待分区设备 %s 超时", dev)
}

func partitionNodeFromSysfs(base string) (string, bool) {
	text := readSys("/sys/class/block/" + base + "/dev")
	if text == "" {
		return "", false
	}
	var major, minor uint64
	if _, err := fmt.Sscanf(text, "%d:%d", &major, &minor); err != nil {
		return "", false
	}
	dev := "/dev/" + base
	mode := uint32(syscall.S_IFBLK | 0o600)
	devno := int((minor & 0xff) | (major << 8) | ((minor &^ 0xff) << 12))
	if err := syscall.Mknod(dev, mode, devno); err != nil {
		if _, statErr := os.Stat(dev); statErr != nil {
			return "", false
		}
	}
	return dev, true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type countingReader struct {
	r     io.Reader
	n     int64
	total int64
	last  time.Time
	cb    func(int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.total > 0 {
		if time.Since(c.last) > 120*time.Millisecond {
			c.last = time.Now()
			pct := int(c.n * 100 / c.total)
			if pct > 99 {
				pct = 99
			}
			if c.cb != nil {
				c.cb(pct)
			}
		}
	}
	return n, err
}

func safeJoin(dest, name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	clean := path.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("不安全的归档路径: %s", name)
	}
	target := filepath.Join(dest, filepath.FromSlash(clean))
	rel, err := filepath.Rel(dest, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("不安全的归档路径: %s", name)
	}
	return target, nil
}

func extractRootfs(archive, dest string, progress func(int, string)) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("打开系统归档失败: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	cr := &countingReader{r: f, total: st.Size(), last: time.Now(), cb: func(pct int) {
		if progress != nil {
			progress(pct, "")
		}
	}}
	gz, err := gzip.NewReader(bufio.NewReaderSize(cr, 1<<20))
	if err != nil {
		return fmt.Errorf("解压系统归档失败: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	lastMsg := time.Now()
	files := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取系统归档失败: %v", err)
		}
		if path.Clean(strings.TrimPrefix(hdr.Name, "./")) == "." {
			continue
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		mode := os.FileMode(hdr.Mode) & 0o777
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			_ = os.Chmod(target, mode)
			_ = os.Lchown(target, 0, 0)
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("写入 %s 失败: %v", hdr.Name, err)
			}
			out.Close()
			_ = os.Chmod(target, mode)
			_ = os.Lchown(target, 0, 0)
			if !hdr.ModTime.IsZero() {
				_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("创建符号链接 %s 失败: %v", hdr.Name, err)
			}
			_ = os.Lchown(target, 0, 0)
		case tar.TypeLink:
			linkTarget, err := safeJoin(dest, hdr.Linkname)
			if err != nil {
				continue
			}
			_ = os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("创建硬链接 %s 失败: %v", hdr.Name, err)
			}
		case tar.TypeFifo:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = syscall.Mkfifo(target, uint32(mode))
		default:
			continue
		}
		files++
		if time.Since(lastMsg) > 2*time.Second && progress != nil {
			lastMsg = time.Now()
			progress(-1, fmt.Sprintf("已解压 %d 个文件", files))
		}
	}
	if progress != nil {
		progress(100, fmt.Sprintf("已解压 %d 个文件", files))
	}
	return nil
}
