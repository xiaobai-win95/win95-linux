//go:build darwin

package disk

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	dkioGetBlockCount = 0x40046419
	dkioGetBlockSize  = 0x40046418
)

func blockDeviceGeometry(f *os.File) (uint64, uint32, error) {
	var count uint64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), dkioGetBlockCount, uintptr(unsafe.Pointer(&count)))
	if errno != 0 {
		return 0, 0, errno
	}
	var logical uint32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), dkioGetBlockSize, uintptr(unsafe.Pointer(&logical)))
	if errno != 0 {
		return 0, 0, errno
	}
	if count != 0 && uint64(logical) > ^uint64(0)/count {
		return 0, 0, fmt.Errorf("block device size overflows uint64")
	}
	return count * uint64(logical), logical, nil
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
	major := (rdev >> 24) & 0xff
	minor := rdev & 0xffffff
	return fmt.Sprintf("%d:%d", major, minor), nil
}

func validatePartitionIdentity(diskFile, partitionFile *os.File, partition Partition) error {
	return errors.New("safe partition formatting is only supported on linux")
}
