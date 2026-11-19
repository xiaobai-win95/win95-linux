package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/tarutil"
)

// ProgressFunc reports one update stage. pct is 0-100, or -1 when only msg is
// available.
type ProgressFunc func(pct int, msg string)

func (p ProgressFunc) report(pct int, msg string) {
	if p != nil {
		p(pct, msg)
	}
}

// UpdateResult describes a slot prepared by ApplyUpdate.
type UpdateResult struct {
	Slot         string
	PreviousSlot string
	Version      string
}

// SHA256File returns the lowercase hex digest of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FetchManifest downloads and validates latest.json.
func FetchManifest(ctx context.Context, url string) (Manifest, error) {
	var m Manifest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return m, err
	}
	req.Header.Set("User-Agent", "win95-linux-ota/1")
	resp, err := otaClient.Do(req)
	if err != nil {
		return m, fmt.Errorf("连接更新服务器失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("更新服务器返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return m, fmt.Errorf("读取更新清单失败: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("解析更新清单失败: %w", err)
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

// DownloadFile downloads url to dest, resuming a partial ".part" file when the
// server supports range requests.
func DownloadFile(ctx context.Context, url, dest string, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
	}
	// The idle watchdog aborts a stalled mirror so the caller can switch to
	// another source without waiting for TCP to give up.
	dlCtx, dlCancel := context.WithCancel(ctx)
	defer dlCancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "win95-linux-ota/1")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := otaClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("下载更新包失败: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusOK:
		offset = 0
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		// A mirror that rejects ranges forces a fresh download.
		resp.Body.Close()
		os.Remove(part)
		return DownloadFile(ctx, url, dest, progress)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("下载更新包失败: HTTP %d", resp.StatusCode)
	}
	idle := time.AfterFunc(45*time.Second, dlCancel)
	defer idle.Stop()
	total := int64(-1)
	if resp.ContentLength >= 0 {
		total = offset + resp.ContentLength
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	done := offset
	last := time.Now()
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(45 * time.Second)
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("下载中断: %w", rerr)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if err := os.Rename(part, dest); err != nil {
		return err
	}
	return nil
}

// FstabContent is the fstab written into every slot. The super partition is
// referenced by PARTUUID because slot A and slot B mount the same filesystem.
func FstabContent(superUUID string) string {
	return fmt.Sprintf("PARTUUID=%s /mnt/win95-super ext4 rw,relatime,nofail 0 0\n"+
		"proc /proc proc defaults 0 0\n"+
		"sysfs /sys sysfs defaults 0 0\n"+
		"devtmpfs /dev devtmpfs mode=0755,nosuid 0 0\n"+
		"devpts /dev/pts devpts gid=5,mode=0620 0 0\n"+
		"tmpfs /tmp tmpfs mode=1777,nosuid,nodev 0 0\n"+
		"tmpfs /run tmpfs mode=0755,nosuid,nodev 0 0\n", superUUID)
}

func WriteFstab(slotDir, superUUID string) error {
	if err := os.MkdirAll(filepath.Join(slotDir, "etc"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(slotDir, "mnt", "win95-super"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(slotDir, "etc", "fstab"), []byte(FstabContent(superUUID)), 0o644)
}

// skipClone lists first-level directories that must never be hardlink-cloned:
// they are virtual filesystems or a live mount of the super partition itself.
func skipClone(rel string) bool {
	switch filepath.ToSlash(rel) {
	case "proc", "sys", "dev", "tmp", "run", "mnt/win95-super":
		return true
	}
	return false
}

// CloneTree recreates src in dst using hardlinks for regular files. Both paths
// must be on the same filesystem. The extracted upgrade later unlinks and
// recreates files, so unchanged files keep sharing storage while changed files
// get their own copy (file-level copy-on-write).
func CloneTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && skipClone(rel) {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return os.Chmod(target, mode.Perm())
		case mode&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			return os.Link(p, target)
		default:
			return nil
		}
	})
}

// WipeDir removes everything inside dir but keeps dir itself.
func WipeDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ReadPackageManifest loads manifest.json from an extracted OTA package.
func ReadPackageManifest(dir string) (PackageManifest, error) {
	var pm PackageManifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return pm, fmt.Errorf("更新包缺少 manifest.json: %w", err)
	}
	if err := json.Unmarshal(data, &pm); err != nil {
		return pm, fmt.Errorf("解析更新包清单失败: %w", err)
	}
	if err := pm.Validate(); err != nil {
		return pm, err
	}
	return pm, nil
}

// ExtractPackage unpacks the OTA tarball into a scratch directory and verifies
// the rootfs digest from its inner manifest.
func ExtractPackage(pkgPath, scratchDir string, progress ProgressFunc) (PackageManifest, string, error) {
	if err := WipeDir(scratchDir); err != nil {
		return PackageManifest{}, "", err
	}
	progress.report(0, "正在解包更新包...")
	if err := tarutil.ExtractTarGz(pkgPath, scratchDir, func(pct int, msg string) {
		if msg == "" {
			progress.report(pct*20/100, "正在解包更新包...")
		}
	}); err != nil {
		return PackageManifest{}, "", err
	}
	pm, err := ReadPackageManifest(scratchDir)
	if err != nil {
		return PackageManifest{}, "", err
	}
	rootfs := filepath.Join(scratchDir, pm.RootfsFile)
	if err := ensureInside(scratchDir, rootfs); err != nil {
		return PackageManifest{}, "", err
	}
	st, err := os.Stat(rootfs)
	if err != nil {
		return PackageManifest{}, "", fmt.Errorf("更新包缺少系统镜像: %w", err)
	}
	if pm.RootfsSize > 0 && st.Size() != pm.RootfsSize {
		return PackageManifest{}, "", fmt.Errorf("系统镜像大小不符（期望 %d，实际 %d）", pm.RootfsSize, st.Size())
	}
	progress.report(20, "正在校验系统镜像...")
	sum, err := SHA256File(rootfs)
	if err != nil {
		return PackageManifest{}, "", err
	}
	if !strings.EqualFold(sum, pm.RootfsSHA256) {
		return PackageManifest{}, "", errors.New("系统镜像 SHA-256 校验失败")
	}
	progress.report(30, "校验完成")
	return pm, rootfs, nil
}

func ensureInside(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("更新包包含不安全的路径")
	}
	return nil
}

// BeginTrial marks slot as the new trial slot; the active slot stays bootable
// until the desktop confirms the update.
func BeginTrial(st State, slot, version string) State {
	ensureSlots(&st)
	slot = NormalizeSlot(slot)
	if slot == "" {
		return st
	}
	st.Slots[slot] = SlotState{Version: version, Status: "trial", Updated: time.Now().UTC().Format(time.RFC3339)}
	st.Trial = slot
	st.Attempts = 0
	st.Booted = ""
	st.LastUpdate = version
	st.LastError = ""
	return st
}

// ApplyUpdate installs an OTA package into the inactive slot. The running slot
// is never modified, so a failed update cannot break the current system.
func ApplyUpdate(superDir, pkgPath, superUUID, currentVersion string, progress ProgressFunc) (UpdateResult, error) {
	var res UpdateResult
	st, err := ReadState(superDir)
	if err != nil {
		return res, fmt.Errorf("读取更新状态失败: %w", err)
	}
	active := NormalizeSlot(st.Active)
	if active == "" {
		active = SlotA
	}
	target := OtherSlot(active)
	res.Slot = target
	res.PreviousSlot = active

	scratch := filepath.Join(superDir, CacheDir, "package")
	pm, rootfs, err := ExtractPackage(pkgPath, scratch, progress)
	if err != nil {
		return res, err
	}
	res.Version = pm.Version
	if currentVersion != "" && !IsNewerVersion(pm.Version, currentVersion) {
		return res, fmt.Errorf("更新包版本 %s 不高于当前版本 %s", pm.Version, currentVersion)
	}
	if superUUID == "" {
		superUUID = st.SuperPartUUID
	}
	if superUUID == "" {
		return res, errors.New("缺少超级分区 PARTUUID，无法生成 fstab")
	}

	progress.report(32, "正在为旧系统建立快照...")
	targetDir := SlotPath(superDir, target)
	if err := WipeDir(targetDir); err != nil {
		return res, err
	}
	if err := CloneTree(SlotPath(superDir, active), targetDir); err != nil {
		return res, fmt.Errorf("复制当前系统快照失败: %w", err)
	}
	syscall.Sync()

	progress.report(40, "正在写入新系统文件...")
	if err := tarutil.ExtractTarGz(rootfs, targetDir, func(pct int, msg string) {
		if msg != "" {
			progress.report(40+pct*45/100, msg)
		} else {
			progress.report(40+pct*45/100, "正在写入新系统文件...")
		}
	}); err != nil {
		return res, err
	}
	progress.report(88, "正在写入启动配置...")
	if err := WriteFstab(targetDir, superUUID); err != nil {
		return res, err
	}
	versionRel := strings.TrimPrefix(VersionFilePath, "/")
	if ReadVersionFile(filepath.Join(targetDir, versionRel)).Version == "" {
		path := filepath.Join(targetDir, versionRel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(path,
			[]byte(VersionFileContent(pm.Version, time.Now().UTC().Format("2006-01-02"), Layout)), 0o644); err != nil {
			return res, err
		}
	}

	progress.report(94, "正在记录更新状态...")
	st = BeginTrial(st, target, pm.Version)
	if st.SuperPartUUID == "" {
		st.SuperPartUUID = strings.ToLower(superUUID)
	}
	if err := WriteState(superDir, st); err != nil {
		return res, err
	}
	syscall.Sync()
	_ = os.RemoveAll(scratch)
	progress.report(100, "更新包已写入插槽 "+strings.ToUpper(target))
	return res, nil
}
