// Package tarutil extracts the rootfs and OTA archives used by the
// installer, the OTA client and the boot images.
package tarutil

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

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
	if c.total > 0 && time.Since(c.last) > 120*time.Millisecond {
		c.last = time.Now()
		pct := int(c.n * 100 / c.total)
		if pct > 99 {
			pct = 99
		}
		if c.cb != nil {
			c.cb(pct)
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

// ExtractTarGz unpacks a gzip-compressed tarball into dest. Regular files are
// unlinked before they are recreated, so extracting an upgrade over a
// hardlinked snapshot triggers file-level copy-on-write instead of rewriting
// the shared inode.
func ExtractTarGz(archive, dest string, progress func(int, string)) error {
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
			_ = os.Remove(target)
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
