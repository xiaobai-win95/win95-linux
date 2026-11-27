//go:build !linux && !darwin

package disk

import (
	"fmt"
	"os"
)

func blockDeviceGeometry(f *os.File) (uint64, uint32, error) {
	end, err := f.Seek(0, 2)
	if err != nil {
		return 0, 0, fmt.Errorf("seek block device: %w", err)
	}
	if end < 0 {
		return 0, 0, fmt.Errorf("invalid block device size %d", end)
	}
	return uint64(end), SectorSize, nil
}

func deviceGeometry(f *os.File, mode os.FileMode) (uint64, uint32, error) {
	if isBlockDevice(mode) {
		return blockDeviceGeometry(f)
	}
	return regularFileGeometry(f)
}

func openedDeviceID(f *os.File) (string, error) {
	return "", nil
}
