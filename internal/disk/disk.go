package disk

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Option configures an edit operation.
type Option interface {
	apply(*options)
}

type options struct {
	allowRegularFile bool
	expectedDeviceID string
}

// Options configures an edit operation. AllowRegularFile is a test-only escape
// hatch; ExpectedDeviceID binds preflight validation to the opened device.
type Options struct {
	AllowRegularFile bool
	ExpectedDeviceID string
}

func (o Options) apply(dst *options) {
	dst.allowRegularFile = o.AllowRegularFile
	dst.expectedDeviceID = o.ExpectedDeviceID
}

type allowRegularFileOption struct{}

func (allowRegularFileOption) apply(dst *options) {
	dst.allowRegularFile = true
}

// AllowRegularFile returns the test-only option that permits edits to a
// synthetic disk image. Without it, every edit requires a block device.
func AllowRegularFile() Option {
	return allowRegularFileOption{}
}

func parseOptions(opts []Option) (options, error) {
	var cfg options
	for _, opt := range opts {
		if opt == nil {
			return cfg, errors.New("nil disk option")
		}
		opt.apply(&cfg)
	}
	return cfg, nil
}

func isBlockDevice(mode os.FileMode) bool {
	return mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0
}

func resolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty device path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve device path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve device path %q: %w", path, err)
	}
	return filepath.Abs(resolved)
}

func readFullAt(f io.ReaderAt, buf []byte, offset int64) error {
	n, err := f.ReadAt(buf, offset)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func readSector(f *os.File, sectorSize uint32, lba uint64) ([]byte, error) {
	if lba > uint64(^uint64(0)>>1)/uint64(sectorSize) {
		return nil, errors.New("sector offset overflows int64")
	}
	buf := make([]byte, int(sectorSize))
	if err := readFullAt(f, buf, int64(lba)*int64(sectorSize)); err != nil {
		return nil, err
	}
	return buf, nil
}

func loadPath(path string) (*loadedDevice, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open device %q: %w", resolved, err)
	}
	defer f.Close()
	return loadDevice(f, resolved)
}

func openEditable(path string, opts []Option) (*os.File, error) {
	cfg, err := parseOptions(opts)
	if err != nil {
		return nil, err
	}
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat device %q: %w", resolved, err)
	}
	if info.Mode().IsRegular() {
		if !cfg.allowRegularFile {
			return nil, fmt.Errorf("refusing to edit regular file %q; use AllowRegularFile only for test images", resolved)
		}
	} else if !isBlockDevice(info.Mode()) {
		return nil, fmt.Errorf("%q is not a block device or regular file", resolved)
	}
	f, err := os.OpenFile(resolved, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open device %q for writing: %w", resolved, err)
	}
	// Recheck the opened object so a path replacement cannot bypass the guard.
	openedInfo, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat opened device: %w", err)
	}
	if openedInfo.Mode().IsRegular() {
		if !cfg.allowRegularFile {
			f.Close()
			return nil, fmt.Errorf("refusing to edit regular file %q", resolved)
		}
	} else if !isBlockDevice(openedInfo.Mode()) {
		f.Close()
		return nil, fmt.Errorf("%q is not a block device or regular file", resolved)
	}
	openedID, err := openedDeviceID(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("identify opened device: %w", err)
	}
	if cfg.expectedDeviceID != "" && openedID != cfg.expectedDeviceID {
		f.Close()
		return nil, fmt.Errorf("device changed after validation: expected %s, opened %s", cfg.expectedDeviceID, openedID)
	}
	return f, nil
}

func loadEditable(path string, opts []Option) (*os.File, *loadedDevice, error) {
	f, err := openEditable(path, opts)
	if err != nil {
		return nil, nil, err
	}
	loaded, err := loadDevice(f, f.Name())
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, loaded, nil
}

type loadedDevice struct {
	device *Device
	rawMBR []byte
	gpt    *gptState
}

func openedDeviceGeometry(f *os.File, path string) (uint64, uint32, uint64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("stat device: %w", err)
	}
	if !info.Mode().IsRegular() && !isBlockDevice(info.Mode()) {
		return 0, 0, 0, fmt.Errorf("%q is not a block device or regular file", path)
	}
	size, sectorSize, err := deviceGeometry(f, info.Mode())
	if err != nil {
		return 0, 0, 0, fmt.Errorf("determine device geometry: %w", err)
	}
	if err := validSectorSize(sectorSize); err != nil {
		return 0, 0, 0, err
	}
	totalSectors := size / uint64(sectorSize)
	if totalSectors == 0 {
		return 0, 0, 0, errors.New("device is too small")
	}
	return size, sectorSize, totalSectors, nil
}

func loadDevice(f *os.File, path string) (*loadedDevice, error) {
	size, sectorSize, totalSectors, err := openedDeviceGeometry(f, path)
	if err != nil {
		return nil, err
	}
	rawMBR, err := readSector(f, sectorSize, 0)
	if err != nil {
		return nil, fmt.Errorf("read MBR: %w", err)
	}
	if totalSectors >= 2 {
		header, headerErr := readSector(f, sectorSize, 1)
		if headerErr != nil {
			return nil, fmt.Errorf("read GPT header: %w", headerErr)
		}
		if string(header[:8]) == "EFI PART" {
			backupLBA := little64(header[32:40])
			if backupLBA == 0 || backupLBA >= totalSectors {
				return nil, fmt.Errorf("invalid GPT backup header LBA %d", backupLBA)
			}
			backupHeader, err := readSector(f, sectorSize, backupLBA)
			if err != nil {
				return nil, fmt.Errorf("read backup GPT header: %w", err)
			}
			loaded, err := readGPT(path, size, sectorSize, totalSectors, rawMBR, header, backupHeader, f)
			if err != nil {
				return nil, err
			}
			if err := setOpenedDeviceID(f, loaded); err != nil {
				return nil, err
			}
			return loaded, nil
		}
	}
	mbr, err := parseMBR(rawMBR, totalSectors)
	if err != nil {
		return nil, fmt.Errorf("parse MBR: %w", err)
	}
	for _, partition := range mbr.Partitions {
		if partition.Type == 0xee {
			return nil, errors.New("protective MBR has no valid GPT header")
		}
	}
	partitions := make([]Partition, 0, len(mbr.Partitions))
	for _, entry := range mbr.Partitions {
		partitions = append(partitions, Partition{
			Number:      entry.Number,
			Table:       TableMBR,
			Type:        entry.Type,
			Status:      entry.Status,
			StartSector: entry.StartLBA,
			EndSector:   entry.EndSector,
			SectorSize:  sectorSize,
		})
	}
	loaded := &loadedDevice{
		device: &Device{
			Path:         path,
			Size:         size,
			SectorSize:   sectorSize,
			TotalSectors: totalSectors,
			Table:        TableMBR,
			MBR:          mbr,
			Partitions:   partitions,
		},
		rawMBR: rawMBR,
	}
	if err := setOpenedDeviceID(f, loaded); err != nil {
		return nil, err
	}
	return loaded, nil
}

func setOpenedDeviceID(f *os.File, loaded *loadedDevice) error {
	id, err := openedDeviceID(f)
	if err != nil {
		return fmt.Errorf("read opened device identity: %w", err)
	}
	loaded.device.DeviceID = id
	return nil
}

func validSectorSize(size uint32) error {
	if size < 512 || size > 4096 || size&(size-1) != 0 {
		return fmt.Errorf("invalid logical sector size %d", size)
	}
	return nil
}

// Inspect reads and validates an MBR or both copies of a GPT. Inspection is
// read-only and accepts regular-file images without an option.
func Inspect(path string, _ ...Option) (*Device, error) {
	loaded, err := loadPath(path)
	if err != nil {
		return nil, err
	}
	return loaded.device, nil
}

func little16(buf []byte) uint16 { return uint16(buf[0]) | uint16(buf[1])<<8 }
func little32(buf []byte) uint32 {
	return uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
}
func little64(buf []byte) uint64 {
	return uint64(little32(buf[:4])) | uint64(little32(buf[4:]))<<32
}
