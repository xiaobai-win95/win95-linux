package disk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BackupTable validates the current table and writes a self-contained JSON
// snapshot to dest. The returned Backup can be passed directly to RestoreTable.
func BackupTable(path, dest string, _ ...Option) (Backup, error) {
	loaded, err := loadPath(path)
	if err != nil {
		return Backup{}, err
	}
	backup := Backup{
		Version:    backupVersion,
		SourcePath: loaded.device.Path,
		Device:     *loaded.device,
		RawMBR:     append([]byte(nil), loaded.rawMBR...),
	}
	backup.Device.Partitions = append([]Partition(nil), loaded.device.Partitions...)
	backup.Device.MBR.Partitions = append([]MBRPartition(nil), loaded.device.MBR.Partitions...)
	if loaded.gpt != nil {
		backup.PrimaryHeader = append([]byte(nil), loaded.gpt.primaryHeader...)
		backup.PrimaryEntries = append([]byte(nil), loaded.gpt.primaryEntries...)
		backup.BackupHeader = append([]byte(nil), loaded.gpt.backupHeader...)
		backup.BackupEntries = append([]byte(nil), loaded.gpt.backupEntries...)
	}
	if err := writeBackupFile(&backup, dest); err != nil {
		return Backup{}, err
	}
	return backup, nil
}

func writeBackupFile(backup *Backup, dest string) error {
	if dest == "" {
		return errors.New("empty backup destination")
	}
	absolute, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolve backup destination: %w", err)
	}
	if sourceInfo, err := os.Stat(backup.SourcePath); err == nil {
		if destInfo, statErr := os.Stat(absolute); statErr == nil && os.SameFile(sourceInfo, destInfo) {
			return errors.New("backup destination is the source device")
		}
	}
	if info, err := os.Stat(absolute); err == nil && info.IsDir() {
		return errors.New("backup destination is a directory")
	}
	dir := filepath.Dir(absolute)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(absolute)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer tmp.Close()
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(backup); err != nil {
		return fmt.Errorf("encode backup: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	if err := os.Rename(tmpName, absolute); err != nil {
		return fmt.Errorf("install backup file: %w", err)
	}
	return nil
}

// RestoreTable restores only partition metadata. It validates the complete
// snapshot, including both GPT copies and their CRC32 values, before writing.
// The destination must have the same logical sector size and sector count; its
// existing table may be damaged or blank.
func RestoreTable(path string, backup Backup, opts ...Option) error {
	f, err := openEditable(path, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	snapshot, err := validateBackup(backup)
	if err != nil {
		return fmt.Errorf("invalid backup: %w", err)
	}
	_, sectorSize, totalSectors, err := openedDeviceGeometry(f, f.Name())
	if err != nil {
		return err
	}
	if sectorSize != snapshot.device.SectorSize {
		return fmt.Errorf("backup sector size is %d but destination is %d", snapshot.device.SectorSize, sectorSize)
	}
	if totalSectors != snapshot.device.TotalSectors {
		return fmt.Errorf("backup has %d sectors but destination has %d", snapshot.device.TotalSectors, totalSectors)
	}
	destinationTable, err := inspectDestinationTable(f, sectorSize, totalSectors)
	if err != nil {
		return err
	}
	if destinationTable != "" && destinationTable != snapshot.device.Table {
		return fmt.Errorf("cannot restore %s partition table to a %s disk", snapshot.device.Table, destinationTable)
	}
	if snapshot.gpt != nil {
		if err := snapshot.gpt.write(f); err != nil {
			return fmt.Errorf("restore GPT: %w", err)
		}
	}
	n, err := f.WriteAt(snapshot.rawMBR, 0)
	if err != nil {
		return fmt.Errorf("restore MBR: %w", err)
	}
	if n != len(snapshot.rawMBR) {
		return fmt.Errorf("short MBR restore: wrote %d of %d bytes", n, len(snapshot.rawMBR))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync restored table: %w", err)
	}
	return nil
}

func inspectDestinationTable(f *os.File, sectorSize uint32, totalSectors uint64) (string, error) {
	var sectors [2][]byte
	for i := range sectors {
		buf := make([]byte, int(sectorSize))
		n, err := f.ReadAt(buf, int64(i)*int64(sectorSize))
		if err != nil && n != len(buf) {
			return "", fmt.Errorf("cannot inspect destination partition table: %w", err)
		}
		sectors[i] = buf
	}
	if string(sectors[1][:8]) == "EFI PART" {
		return TableGPT, nil
	}
	if totalSectors > 0 {
		last, err := readSector(f, sectorSize, totalSectors-1)
		if err != nil {
			return "", fmt.Errorf("cannot inspect backup partition table: %w", err)
		}
		if string(last[:8]) == "EFI PART" {
			return TableGPT, nil
		}
	}
	if bytes.Equal(sectors[0], make([]byte, int(sectorSize))) && bytes.Equal(sectors[1], make([]byte, int(sectorSize))) {
		return "", nil
	}
	for i := 0; i < 4; i++ {
		if sectors[0][mbrEntryOff+i*16+4] == 0xee {
			return TableGPT, nil
		}
	}
	if little16(sectors[0][510:512]) == mbrSignature {
		return TableMBR, nil
	}
	return "", errors.New("cannot determine destination partition table")
}

func validateBackup(backup Backup) (*loadedDevice, error) {
	if backup.Version != backupVersion {
		return nil, fmt.Errorf("unsupported backup version %d", backup.Version)
	}
	if err := validSectorSize(backup.Device.SectorSize); err != nil {
		return nil, err
	}
	if backup.Device.TotalSectors < 2 {
		return nil, errors.New("backup device is too small")
	}
	if backup.Device.Table != TableGPT && backup.Device.Table != TableMBR {
		return nil, fmt.Errorf("unknown backup table type %q", backup.Device.Table)
	}
	if uint64(len(backup.RawMBR)) != uint64(backup.Device.SectorSize) {
		return nil, errors.New("backup MBR has the wrong size")
	}
	size := backup.Device.TotalSectors * uint64(backup.Device.SectorSize)
	if size/uint64(backup.Device.SectorSize) != backup.Device.TotalSectors {
		return nil, errors.New("backup device size overflows")
	}
	if backup.Device.Table == TableMBR {
		if len(backup.PrimaryHeader) != 0 || len(backup.PrimaryEntries) != 0 || len(backup.BackupHeader) != 0 || len(backup.BackupEntries) != 0 {
			return nil, errors.New("MBR backup contains GPT metadata")
		}
		mbr, err := parseMBR(backup.RawMBR, backup.Device.TotalSectors)
		if err != nil {
			return nil, err
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
				SectorSize:  backup.Device.SectorSize,
			})
		}
		return &loadedDevice{
			device: &Device{
				Path:         backup.SourcePath,
				Size:         size,
				SectorSize:   backup.Device.SectorSize,
				TotalSectors: backup.Device.TotalSectors,
				Table:        TableMBR,
				MBR:          mbr,
				Partitions:   partitions,
			},
			rawMBR: append([]byte(nil), backup.RawMBR...),
		}, nil
	}
	if len(backup.PrimaryHeader) != int(backup.Device.SectorSize) || len(backup.BackupHeader) != int(backup.Device.SectorSize) {
		return nil, errors.New("backup GPT headers have the wrong size")
	}
	if len(backup.PrimaryEntries) == 0 || len(backup.PrimaryEntries) != len(backup.BackupEntries) {
		return nil, errors.New("backup GPT entry arrays have the wrong size")
	}
	return assembleGPT(
		backup.SourcePath,
		size,
		backup.Device.SectorSize,
		backup.Device.TotalSectors,
		backup.RawMBR,
		backup.PrimaryHeader,
		backup.BackupHeader,
		backup.PrimaryEntries,
		backup.BackupEntries,
	)
}
