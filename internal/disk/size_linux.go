//go:build linux

package disk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func validatePartitionIdentity(diskFile, partitionFile *os.File, partition Partition) error {
	diskID, err := openedDeviceID(diskFile)
	if err != nil {
		return err
	}
	partitionID, err := openedDeviceID(partitionFile)
	if err != nil {
		return err
	}
	if diskID == "" || partitionID == "" || diskID == partitionID {
		return errors.New("cannot identify partition parent device")
	}
	diskSys, err := filepath.EvalSymlinks(filepath.Join("/sys/dev/block", diskID))
	if err != nil {
		return fmt.Errorf("resolve disk sysfs identity: %w", err)
	}
	partitionSys, err := filepath.EvalSymlinks(filepath.Join("/sys/dev/block", partitionID))
	if err != nil {
		return fmt.Errorf("resolve partition sysfs identity: %w", err)
	}
	return validateLinuxPartitionPaths(diskSys, partitionSys, partition)
}

func validateLinuxPartitionPaths(diskSys, partitionSys string, partition Partition) error {
	if !strings.HasPrefix(partitionSys, diskSys+string(os.PathSeparator)) {
		return errors.New("partition does not belong to the selected disk")
	}
	numberData, err := os.ReadFile(filepath.Join(partitionSys, "partition"))
	if err != nil {
		return fmt.Errorf("read partition number: %w", err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(numberData)))
	if err != nil || number != partition.Number {
		return fmt.Errorf("partition number is %q, want %d", strings.TrimSpace(string(numberData)), partition.Number)
	}
	startData, err := os.ReadFile(filepath.Join(partitionSys, "start"))
	if err != nil {
		return fmt.Errorf("read partition start: %w", err)
	}
	start512, err := strconv.ParseUint(strings.TrimSpace(string(startData)), 10, 64)
	if err != nil {
		return fmt.Errorf("parse partition start: %w", err)
	}
	sectorSize := uint64(partition.SectorSize)
	if sectorSize == 0 {
		sectorSize = SectorSize
	}
	if partition.StartSector > ^uint64(0)/sectorSize || start512 > ^uint64(0)/512 {
		return errors.New("partition start overflows")
	}
	if start512*512 != partition.StartSector*sectorSize {
		return fmt.Errorf("partition starts at %d bytes, table expects %d", start512*512, partition.StartSector*sectorSize)
	}
	return nil
}
