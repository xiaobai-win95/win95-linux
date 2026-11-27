package disk

import (
	"fmt"
	"io"
	"os"
)

func regularFileGeometry(f *os.File) (uint64, uint32, error) {
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, fmt.Errorf("get file offset: %w", err)
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, fmt.Errorf("get file size: %w", err)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, 0, fmt.Errorf("restore file offset: %w", err)
	}
	if end <= 0 {
		return 0, 0, fmt.Errorf("device size is %d bytes", end)
	}
	return uint64(end), SectorSize, nil
}
