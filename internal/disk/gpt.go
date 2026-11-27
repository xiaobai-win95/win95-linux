package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
)

const (
	gptSignature     = "EFI PART"
	gptRevision      = 0x00010000
	maxGPTTableBytes = 64 << 20
)

type gptHeader struct {
	revision    uint32
	headerSize  uint32
	currentLBA  uint64
	backupLBA   uint64
	firstUsable uint64
	lastUsable  uint64
	diskGUID    [16]byte
	entriesLBA  uint64
	entryCount  uint32
	entrySize   uint32
	entriesCRC  uint32
}

type gptState struct {
	sectorSize     uint32
	primaryHeader  []byte
	backupHeader   []byte
	primaryEntries []byte
	backupEntries  []byte
	info           GPT
}

func parseGPTHeader(raw []byte, sectorSize uint32, totalSectors, expectedCurrent uint64) (gptHeader, error) {
	var header gptHeader
	if len(raw) < int(sectorSize) || len(raw) < gptHeaderSize {
		return header, errors.New("short GPT header")
	}
	if string(raw[:8]) != gptSignature {
		return header, errors.New("invalid GPT signature")
	}
	header.revision = little32(raw[8:12])
	if header.revision != gptRevision {
		return header, fmt.Errorf("unsupported GPT revision 0x%08x", header.revision)
	}
	header.headerSize = little32(raw[12:16])
	if header.headerSize < gptHeaderSize || header.headerSize > sectorSize {
		return header, fmt.Errorf("invalid GPT header size %d", header.headerSize)
	}
	checksum := append([]byte(nil), raw[:header.headerSize]...)
	for i := 16; i < 20; i++ {
		checksum[i] = 0
	}
	if got, want := crc32.ChecksumIEEE(checksum), little32(raw[16:20]); got != want {
		return header, fmt.Errorf("GPT header CRC32 mismatch: got %08x, want %08x", got, want)
	}
	header.currentLBA = little64(raw[24:32])
	header.backupLBA = little64(raw[32:40])
	header.firstUsable = little64(raw[40:48])
	header.lastUsable = little64(raw[48:56])
	copy(header.diskGUID[:], raw[56:72])
	header.entriesLBA = little64(raw[72:80])
	header.entryCount = little32(raw[80:84])
	header.entrySize = little32(raw[84:88])
	header.entriesCRC = little32(raw[88:92])

	if header.currentLBA != expectedCurrent {
		return header, fmt.Errorf("GPT current LBA is %d, want %d", header.currentLBA, expectedCurrent)
	}
	if header.backupLBA == 0 || header.backupLBA >= totalSectors {
		return header, fmt.Errorf("invalid GPT alternate header LBA %d", header.backupLBA)
	}
	if zeroGUID(header.diskGUID) {
		return header, errors.New("GPT disk GUID is zero")
	}
	usableLimit := header.backupLBA
	if expectedCurrent > 1 {
		usableLimit = expectedCurrent
	}
	if header.firstUsable <= 1 || header.lastUsable < header.firstUsable || header.lastUsable >= usableLimit {
		return header, errors.New("invalid GPT usable-sector range")
	}
	if header.entryCount == 0 || header.entrySize < gptEntrySize || header.entrySize > 4096 || header.entrySize%gptEntrySize != 0 {
		return header, errors.New("invalid GPT entry-array parameters")
	}
	multiplier := header.entrySize / gptEntrySize
	if multiplier&(multiplier-1) != 0 {
		return header, errors.New("GPT entry size is not a power-of-two multiple of 128")
	}
	tableBytes := uint64(header.entryCount) * uint64(header.entrySize)
	if tableBytes > maxGPTTableBytes {
		return header, fmt.Errorf("GPT entry array is too large (%d bytes)", tableBytes)
	}
	entrySectors := (tableBytes + uint64(sectorSize) - 1) / uint64(sectorSize)
	if header.entriesLBA == 0 || entrySectors == 0 || header.entriesLBA > ^uint64(0)-(entrySectors-1) {
		return header, errors.New("invalid GPT entry-array location")
	}
	entryEnd := header.entriesLBA + entrySectors - 1
	if entryEnd >= totalSectors {
		return header, errors.New("GPT entry array extends beyond the device")
	}
	if expectedCurrent == 1 {
		if header.entriesLBA < 2 || entryEnd >= header.firstUsable {
			return header, errors.New("primary GPT entry array overlaps usable space")
		}
	} else if header.entriesLBA <= header.lastUsable || entryEnd >= expectedCurrent {
		return header, errors.New("backup GPT entry array overlaps usable space")
	}
	return header, nil
}

func validateGPTHeaderPair(primary, backup gptHeader, totalSectors uint64) error {
	if primary.currentLBA != 1 || primary.backupLBA != totalSectors-1 {
		return errors.New("primary GPT header is not at the expected location")
	}
	if backup.currentLBA != primary.backupLBA || backup.backupLBA != primary.currentLBA {
		return errors.New("GPT primary and backup headers do not reference each other")
	}
	if primary.revision != backup.revision || primary.headerSize != backup.headerSize || primary.diskGUID != backup.diskGUID {
		return errors.New("GPT primary and backup header identities differ")
	}
	if primary.firstUsable != backup.firstUsable || primary.lastUsable != backup.lastUsable {
		return errors.New("GPT primary and backup usable ranges differ")
	}
	if primary.entryCount != backup.entryCount || primary.entrySize != backup.entrySize || primary.entriesCRC != backup.entriesCRC {
		return errors.New("GPT primary and backup entry-array parameters differ")
	}
	if primary.entriesLBA == backup.entriesLBA {
		return errors.New("GPT primary and backup entry arrays have the same location")
	}
	return nil
}

func parseGPTEntries(raw []byte, header gptHeader, sectorSize uint32) ([]Partition, error) {
	wantBytes := uint64(header.entryCount) * uint64(header.entrySize)
	if uint64(len(raw)) != wantBytes {
		return nil, fmt.Errorf("GPT entry array has %d bytes, want %d", len(raw), wantBytes)
	}
	partitions := make([]Partition, 0)
	ranges := make([]sectorRange, 0)
	uniqueGUIDs := make(map[string]struct{})
	for i := uint32(0); i < header.entryCount; i++ {
		entry := raw[uint64(i)*uint64(header.entrySize):]
		entry = entry[:header.entrySize]
		var typeGUID, uniqueGUID [16]byte
		copy(typeGUID[:], entry[0:16])
		if zeroGUID(typeGUID) {
			continue
		}
		copy(uniqueGUID[:], entry[16:32])
		if zeroGUID(uniqueGUID) {
			return nil, fmt.Errorf("GPT partition %d has a zero unique GUID", i+1)
		}
		uniqueGUIDText := formatGUID(uniqueGUID)
		if _, exists := uniqueGUIDs[uniqueGUIDText]; exists {
			return nil, fmt.Errorf("GPT partition %d duplicates unique GUID %s", i+1, uniqueGUIDText)
		}
		uniqueGUIDs[uniqueGUIDText] = struct{}{}
		start := little64(entry[32:40])
		end := little64(entry[40:48])
		if start < header.firstUsable || end > header.lastUsable || end < start {
			return nil, fmt.Errorf("GPT partition %d has an invalid sector range", i+1)
		}
		partitions = append(partitions, Partition{
			Number:      int(i) + 1,
			Table:       TableGPT,
			Name:        decodeGPTName(entry[56:128]),
			TypeGUID:    formatGUID(typeGUID),
			UniqueGUID:  uniqueGUIDText,
			StartSector: start,
			EndSector:   end,
			Attributes:  little64(entry[48:56]),
			SectorSize:  sectorSize,
		})
		ranges = append(ranges, sectorRange{int(i) + 1, start, end})
	}
	if err := validateRanges(ranges); err != nil {
		return nil, err
	}
	return partitions, nil
}

func readGPT(path string, size uint64, sectorSize uint32, totalSectors uint64, rawMBR, rawPrimaryHeader, rawBackupHeader []byte, f *os.File) (*loadedDevice, error) {
	mbr, err := parseMBR(rawMBR, totalSectors)
	if err != nil {
		return nil, fmt.Errorf("parse protective MBR: %w", err)
	}
	if err := validateProtectiveMBR(mbr, totalSectors); err != nil {
		return nil, err
	}
	primary, err := parseGPTHeader(rawPrimaryHeader, sectorSize, totalSectors, 1)
	if err != nil {
		return nil, fmt.Errorf("parse primary GPT header: %w", err)
	}
	backup, err := parseGPTHeader(rawBackupHeader, sectorSize, totalSectors, totalSectors-1)
	if err != nil {
		return nil, fmt.Errorf("parse backup GPT header: %w", err)
	}
	if err := validateGPTHeaderPair(primary, backup, totalSectors); err != nil {
		return nil, err
	}
	entryBytes := uint64(primary.entryCount) * uint64(primary.entrySize)
	primaryEntries := make([]byte, int(entryBytes))
	backupEntries := make([]byte, int(entryBytes))
	if err := readFullAt(f, primaryEntries, int64(primary.entriesLBA)*int64(sectorSize)); err != nil {
		return nil, fmt.Errorf("read primary GPT entries: %w", err)
	}
	if err := readFullAt(f, backupEntries, int64(backup.entriesLBA)*int64(sectorSize)); err != nil {
		return nil, fmt.Errorf("read backup GPT entries: %w", err)
	}
	return assembleGPT(path, size, sectorSize, totalSectors, rawMBR, rawPrimaryHeader, rawBackupHeader, primaryEntries, backupEntries)
}

func assembleGPT(path string, size uint64, sectorSize uint32, totalSectors uint64, rawMBR, rawPrimaryHeader, rawBackupHeader, primaryEntries, backupEntries []byte) (*loadedDevice, error) {
	mbr, err := parseMBR(rawMBR, totalSectors)
	if err != nil {
		return nil, fmt.Errorf("parse protective MBR: %w", err)
	}
	if err := validateProtectiveMBR(mbr, totalSectors); err != nil {
		return nil, err
	}
	primary, err := parseGPTHeader(rawPrimaryHeader, sectorSize, totalSectors, 1)
	if err != nil {
		return nil, fmt.Errorf("parse primary GPT header: %w", err)
	}
	backup, err := parseGPTHeader(rawBackupHeader, sectorSize, totalSectors, totalSectors-1)
	if err != nil {
		return nil, fmt.Errorf("parse backup GPT header: %w", err)
	}
	if err := validateGPTHeaderPair(primary, backup, totalSectors); err != nil {
		return nil, err
	}
	if got, want := crc32.ChecksumIEEE(primaryEntries), primary.entriesCRC; got != want {
		return nil, fmt.Errorf("primary GPT entries CRC32 mismatch: got %08x, want %08x", got, want)
	}
	if got, want := crc32.ChecksumIEEE(backupEntries), backup.entriesCRC; got != want {
		return nil, fmt.Errorf("backup GPT entries CRC32 mismatch: got %08x, want %08x", got, want)
	}
	if !bytes.Equal(primaryEntries, backupEntries) {
		return nil, errors.New("primary and backup GPT entry arrays differ")
	}
	partitions, err := parseGPTEntries(primaryEntries, primary, sectorSize)
	if err != nil {
		return nil, err
	}
	info := GPT{
		Revision:            primary.revision,
		HeaderSize:          primary.headerSize,
		DiskGUID:            formatGUID(primary.diskGUID),
		PrimaryHeaderLBA:    primary.currentLBA,
		BackupHeaderLBA:     primary.backupLBA,
		FirstUsableLBA:      primary.firstUsable,
		LastUsableLBA:       primary.lastUsable,
		PrimaryEntriesLBA:   primary.entriesLBA,
		BackupEntriesLBA:    backup.entriesLBA,
		EntryCount:          primary.entryCount,
		EntrySize:           primary.entrySize,
		PrimaryEntriesCRC32: primary.entriesCRC,
		BackupEntriesCRC32:  backup.entriesCRC,
	}
	return &loadedDevice{
		device: &Device{
			Path:         path,
			Size:         size,
			SectorSize:   sectorSize,
			TotalSectors: totalSectors,
			Table:        TableGPT,
			MBR:          mbr,
			Partitions:   partitions,
			GPT:          &info,
		},
		rawMBR: append([]byte(nil), rawMBR...),
		gpt: &gptState{
			sectorSize:     sectorSize,
			primaryHeader:  append([]byte(nil), rawPrimaryHeader...),
			backupHeader:   append([]byte(nil), rawBackupHeader...),
			primaryEntries: primaryEntries,
			backupEntries:  backupEntries,
			info:           info,
		},
	}, nil
}

func (state *gptState) refreshChecksums() {
	state.backupEntries = append(state.backupEntries[:0], state.primaryEntries...)
	entriesCRC := crc32.ChecksumIEEE(state.primaryEntries)
	for _, header := range [][]byte{state.primaryHeader, state.backupHeader} {
		binary.LittleEndian.PutUint32(header[88:92], entriesCRC)
		for i := 16; i < 20; i++ {
			header[i] = 0
		}
		binary.LittleEndian.PutUint32(header[16:20], crc32.ChecksumIEEE(header[:little32(header[12:16])]))
	}
	state.info.PrimaryEntriesCRC32 = entriesCRC
	state.info.BackupEntriesCRC32 = entriesCRC
}

type gptWriteIO interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Sync() error
}

type gptWriteChunk struct {
	name   string
	data   []byte
	offset int64
}

func (state *gptState) validateForWrite() error {
	if state.info.BackupHeaderLBA == ^uint64(0) {
		return errors.New("GPT backup header LBA overflows the sector count")
	}
	totalSectors := state.info.BackupHeaderLBA + 1
	primary, err := parseGPTHeader(state.primaryHeader, state.sectorSize, totalSectors, 1)
	if err != nil {
		return fmt.Errorf("validate primary GPT header: %w", err)
	}
	backup, err := parseGPTHeader(state.backupHeader, state.sectorSize, totalSectors, totalSectors-1)
	if err != nil {
		return fmt.Errorf("validate backup GPT header: %w", err)
	}
	if err := validateGPTHeaderPair(primary, backup, totalSectors); err != nil {
		return err
	}
	if primary.currentLBA != state.info.PrimaryHeaderLBA || backup.currentLBA != state.info.BackupHeaderLBA ||
		primary.entriesLBA != state.info.PrimaryEntriesLBA || backup.entriesLBA != state.info.BackupEntriesLBA {
		return errors.New("GPT header locations changed during update")
	}
	if !bytes.Equal(state.primaryEntries, state.backupEntries) {
		return errors.New("primary and backup GPT entry arrays differ")
	}
	if crc32.ChecksumIEEE(state.primaryEntries) != primary.entriesCRC || crc32.ChecksumIEEE(state.backupEntries) != backup.entriesCRC {
		return errors.New("GPT entry-array checksums do not match the headers")
	}
	_, err = parseGPTEntries(state.primaryEntries, primary, state.sectorSize)
	return err
}

func gptByteOffset(lba uint64, sectorSize uint32) (int64, error) {
	if sectorSize == 0 || lba > uint64(^uint64(0)>>1)/uint64(sectorSize) {
		return 0, errors.New("GPT sector offset overflows int64")
	}
	return int64(lba) * int64(sectorSize), nil
}

// write commits the backup copy first and the primary copy last. Every write is
// synced before the next metadata object is changed, so each completed stage
// leaves at least one valid GPT copy. The exact previous bytes are retained for
// rollback on any reported write or sync failure; rollback errors are joined and
// never hidden.
func (state *gptState) write(f gptWriteIO) error {
	state.refreshChecksums()
	if err := state.validateForWrite(); err != nil {
		return fmt.Errorf("refuse invalid GPT update: %w", err)
	}
	writes := []gptWriteChunk{
		{name: "backup entries", data: state.backupEntries},
		{name: "backup header", data: state.backupHeader},
		{name: "primary entries", data: state.primaryEntries},
		{name: "primary header", data: state.primaryHeader},
	}
	locations := []uint64{state.info.BackupEntriesLBA, state.info.BackupHeaderLBA, state.info.PrimaryEntriesLBA, state.info.PrimaryHeaderLBA}
	for i := range writes {
		offset, err := gptByteOffset(locations[i], state.sectorSize)
		if err != nil {
			return err
		}
		writes[i].offset = offset
	}
	previous := make([]gptWriteChunk, len(writes))
	for i, write := range writes {
		data := make([]byte, len(write.data))
		if err := readFullAt(f, data, write.offset); err != nil {
			return fmt.Errorf("back up previous GPT %s: %w", write.name, err)
		}
		previous[i] = gptWriteChunk{name: write.name, data: data, offset: write.offset}
	}
	if err := writeGPTChunks(f, writes); err != nil {
		writeErr := fmt.Errorf("write GPT metadata: %w", err)
		if rollbackErr := writeGPTChunks(f, previous); rollbackErr != nil {
			return errors.Join(writeErr, fmt.Errorf("rollback GPT metadata: %w", rollbackErr))
		}
		return fmt.Errorf("%w; previous GPT metadata restored", writeErr)
	}
	return nil
}

func writeGPTChunks(f gptWriteIO, chunks []gptWriteChunk) error {
	for _, chunk := range chunks {
		n, err := f.WriteAt(chunk.data, chunk.offset)
		if err != nil {
			return fmt.Errorf("%s: %w", chunk.name, err)
		}
		if n != len(chunk.data) {
			return fmt.Errorf("%s: short write: wrote %d of %d bytes", chunk.name, n, len(chunk.data))
		}
		if err := f.Sync(); err != nil {
			return fmt.Errorf("sync %s: %w", chunk.name, err)
		}
	}
	return nil
}

func setGPTEntry(entries []byte, slot, entrySize int, typeGUID, uniqueGUID [16]byte, start, end, attributes uint64, name string) error {
	entry := entries[(slot-1)*entrySize : slot*entrySize]
	for i := range entry {
		entry[i] = 0
	}
	if typeGUID != [16]byte{} {
		copy(entry[0:16], typeGUID[:])
		copy(entry[16:32], uniqueGUID[:])
		binary.LittleEndian.PutUint64(entry[32:40], start)
		binary.LittleEndian.PutUint64(entry[40:48], end)
		binary.LittleEndian.PutUint64(entry[48:56], attributes)
		return encodeGPTName(entry[56:128], name)
	}
	return nil
}
