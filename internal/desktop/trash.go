package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// trashRoot is a variable so tests can redirect the trash to a temp folder.
var trashRoot = trashDir

// moveToTrash moves path into the trash, falling back to copy+remove when the
// trash lives on a different filesystem (for example tmpfs /tmp).
func moveToTrash(path string) (string, error) {
	trash := trashRoot
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(path)
	dest := filepath.Join(trash, base)
	for i := 1; i < 10000; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		if i == 1 {
			dest = filepath.Join(trash, fmt.Sprintf("%s-%d", base, time.Now().Unix()))
		} else {
			dest = filepath.Join(trash, fmt.Sprintf("%s-%d-%d", base, time.Now().Unix(), i))
		}
	}
	err := os.Rename(path, dest)
	if err == nil {
		return dest, nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return "", err
	}
	if err := copyTree(path, dest); err != nil {
		_ = os.RemoveAll(dest)
		return "", err
	}
	if err := os.RemoveAll(path); err != nil {
		return dest, err
	}
	return dest, nil
}

func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case mode.IsDir():
		if err := os.Mkdir(dst, mode.Perm()); err != nil {
			return err
		}
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dst, mode.Perm())
	case mode.IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	default:
		return fmt.Errorf("不支持移动特殊文件：%s", src)
	}
}
