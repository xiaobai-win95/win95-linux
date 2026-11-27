//go:build linux

package disk

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	blkgetsize64 = 0x80081272
	blksszget    = 0x1268
)

func blockDeviceGeometry(f *os.File) (uint64, uint32, error) {
	var size uint64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), blkgetsize64, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, 0, errno
	}
	var logical int32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), blksszget, uintptr(unsafe.Pointer(&logical)))
	if errno != 0 || logical <= 0 {
		logical = SectorSize
	}
	if logical > 4096 {
		return 0, 0, fmt.Errorf("unsupported logical sector size %d", logical)
	}
	return size, uint32(logical), nil
}

func deviceGeometry(f *os.File, mode os.FileMode) (uint64, uint32, error) {
	if isBlockDevice(mode) {
		return blockDeviceGeometry(f)
	}
	return regularFileGeometry(f)
}

func openedDeviceID(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeDevice == 0 {
		return "", nil
	}
	rdev := uint64(stat.Rdev)
	major := (rdev >> 8) & 0xfff
	major |= (rdev >> 32) & 0xfffff000
	minor := rdev & 0xff
	minor |= (rdev >> 12) & 0xfffff00
	return fmt.Sprintf("%d:%d", major, minor), nil
}
