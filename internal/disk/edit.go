package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// CreatePartition adds a GPT entry or primary MBR entry. It never initializes
// a filesystem and never moves existing partitions.
func CreatePartition(path string, spec PartitionSpec, opts ...Option) error {
	f, loaded, err := loadEditable(path, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	switch loaded.device.Table {
	case TableGPT:
		return createGPTPartition(f, loaded, spec)
	case TableMBR:
		return createMBRPartition(f, loaded, spec)
	default:
		return fmt.Errorf("unsupported table type %q", loaded.device.Table)
	}
}

func createGPTPartition(f *os.File, loaded *loadedDevice, spec PartitionSpec) error {
	if spec.Type != 0 {
		return errors.New("MBR partition type is not valid for GPT partitions")
	}
	state := loaded.gpt
	start, end, err := resolvePartitionRange(spec, state.info.FirstUsableLBA, state.info.LastUsableLBA)
	if err != nil {
		return err
	}
	if err := checkNewPartitionOverlap(spec.Number, start, end, loaded.device.Partitions); err != nil {
		return err
	}
	if err := encodeGPTName(make([]byte, gptEntrySize-56), spec.Name); err != nil {
		return err
	}
	typeGUID, err := parseGUID(spec.TypeGUID)
	if err != nil {
		return err
	}
	if zeroGUID(typeGUID) {
		return errors.New("GPT partition type GUID cannot be zero")
	}
	uniqueGUID, err := randomGUID()
	if err != nil {
		return err
	}
	for _, partition := range loaded.device.Partitions {
		if partition.UniqueGUID == formatGUID(uniqueGUID) {
			return errors.New("generated a duplicate partition GUID")
		}
	}
	slot, err := freeGPTSlot(state, spec.Number)
	if err != nil {
		return err
	}
	if err := setGPTEntry(state.primaryEntries, slot, int(state.info.EntrySize), typeGUID, uniqueGUID, start, end, spec.Attributes, spec.Name); err != nil {
		return err
	}
	if err := state.write(f); err != nil {
		return fmt.Errorf("write GPT after create: %w", err)
	}
	return nil
}

func createMBRPartition(f *os.File, loaded *loadedDevice, spec PartitionSpec) error {
	if spec.Name != "" {
		return errors.New("MBR partition names are not supported")
	}
	if spec.TypeGUID != "" || spec.Attributes != 0 {
		return errors.New("GPT type GUID and attributes are not valid for MBR partitions")
	}
	start, end, err := resolvePartitionRange(spec, 1, loaded.device.TotalSectors-1)
	if err != nil {
		return err
	}
	if err := checkNewPartitionOverlap(spec.Number, start, end, loaded.device.Partitions); err != nil {
		return err
	}
	count := end - start + 1
	if count > 0xffffffff || start > 0xffffffff {
		return errors.New("MBR partition exceeds the 32-bit LBA format")
	}
	partitionType := spec.Type
	if partitionType == 0 {
		partitionType = 0x83
	}
	if partitionType == 0xee {
		return errors.New("0xee is reserved for a GPT protective MBR")
	}
	slot := spec.Number
	if slot == 0 {
		for candidate := 1; candidate <= 4; candidate++ {
			if loaded.rawMBR[mbrEntryOff+(candidate-1)*16+4] == 0 {
				slot = candidate
				break
			}
		}
	}
	if slot < 1 || slot > 4 {
		return errors.New("MBR partition number must be between 1 and 4")
	}
	if loaded.rawMBR[mbrEntryOff+(slot-1)*16+4] != 0 {
		return fmt.Errorf("MBR partition %d already exists", slot)
	}
	setMBRPartition(loaded.rawMBR, slot, 0, partitionType, start, count)
	if err := writeMBR(f, loaded.rawMBR); err != nil {
		return fmt.Errorf("write MBR after create: %w", err)
	}
	return nil
}

// DeletePartition clears a GPT or primary MBR entry. Partition data is left
// untouched so a later table restore can make it visible again.
func DeletePartition(path string, number int, opts ...Option) error {
	f, loaded, err := loadEditable(path, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	if number <= 0 {
		return errors.New("partition number must be positive")
	}
	switch loaded.device.Table {
	case TableGPT:
		state := loaded.gpt
		if number > int(state.info.EntryCount) {
			return fmt.Errorf("GPT partition %d does not exist", number)
		}
		entry := loaded.gpt.primaryEntries[(number-1)*int(state.info.EntrySize) : number*int(state.info.EntrySize)]
		if zeroGUID([16]byte(entry[0:16])) {
			return fmt.Errorf("GPT partition %d does not exist", number)
		}
		for i := range entry {
			entry[i] = 0
		}
		if err := state.write(f); err != nil {
			return fmt.Errorf("write GPT after delete: %w", err)
		}
	case TableMBR:
		if number > 4 || loaded.rawMBR[mbrEntryOff+(number-1)*16+4] == 0 {
			return fmt.Errorf("MBR partition %d does not exist", number)
		}
		entry := loaded.rawMBR[mbrEntryOff+(number-1)*16 : mbrEntryOff+number*16]
		for i := range entry {
			entry[i] = 0
		}
		if err := writeMBR(f, loaded.rawMBR); err != nil {
			return fmt.Errorf("write MBR after delete: %w", err)
		}
	default:
		return fmt.Errorf("unsupported table type %q", loaded.device.Table)
	}
	return nil
}

// ResizePartition changes only a partition's inclusive end sector. It checks
// alignment and neighboring partitions but does not resize a filesystem.
func ResizePartition(path string, number int, newEndSector uint64, opts ...Option) error {
	f, loaded, err := loadEditable(path, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	partition, ok := findPartition(loaded.device.Partitions, number)
	if !ok {
		return fmt.Errorf("partition %d does not exist", number)
	}
	switch loaded.device.Table {
	case TableGPT:
		state := loaded.gpt
		if err := validateResizeRange(partition.StartSector, newEndSector, state.info.FirstUsableLBA, state.info.LastUsableLBA, loaded.device.Partitions, number); err != nil {
			return err
		}
		entry := state.primaryEntries[(number-1)*int(state.info.EntrySize) : number*int(state.info.EntrySize)]
		binary.LittleEndian.PutUint64(entry[40:48], newEndSector)
		if err := state.write(f); err != nil {
			return fmt.Errorf("write GPT after resize: %w", err)
		}
	case TableMBR:
		if err := validateResizeRange(partition.StartSector, newEndSector, 1, loaded.device.TotalSectors-1, loaded.device.Partitions, number); err != nil {
			return err
		}
		count := newEndSector - partition.StartSector + 1
		if count > 0xffffffff || partition.StartSector > 0xffffffff || newEndSector > 0xffffffff {
			return errors.New("resized MBR partition exceeds the 32-bit LBA format")
		}
		setMBRPartition(loaded.rawMBR, number, partition.Status, partition.Type, partition.StartSector, count)
		if err := writeMBR(f, loaded.rawMBR); err != nil {
			return fmt.Errorf("write MBR after resize: %w", err)
		}
	default:
		return fmt.Errorf("unsupported table type %q", loaded.device.Table)
	}
	return nil
}

func resolvePartitionRange(spec PartitionSpec, firstUsable, lastUsable uint64) (uint64, uint64, error) {
	start := spec.StartSector
	end := spec.EndSector
	if end == 0 {
		if spec.SizeSectors == 0 {
			return 0, 0, errors.New("partition spec requires EndSector or SizeSectors")
		}
		if spec.SizeSectors-1 > ^uint64(0)-start {
			return 0, 0, errors.New("partition range overflows")
		}
		end = start + spec.SizeSectors - 1
	} else {
		if end < start {
			return 0, 0, errors.New("partition end precedes its start")
		}
		if spec.SizeSectors != 0 && spec.SizeSectors != end-start+1 {
			return 0, 0, errors.New("EndSector and SizeSectors describe different ranges")
		}
	}
	if err := validateAlignedRange(start, end, firstUsable, lastUsable); err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func validateAlignedRange(start, end, firstUsable, lastUsable uint64) error {
	if start < firstUsable || end > lastUsable || end < start {
		return fmt.Errorf("partition range %d-%d is outside usable sectors %d-%d", start, end, firstUsable, lastUsable)
	}
	if start%AlignmentSectors != 0 {
		return fmt.Errorf("partition start %d is not aligned to %d sectors", start, AlignmentSectors)
	}
	if end == ^uint64(0) || (end+1)%AlignmentSectors != 0 {
		return fmt.Errorf("partition end %d is not aligned to %d sectors", end, AlignmentSectors)
	}
	return nil
}

func validateResizeRange(start, end, firstUsable, lastUsable uint64, partitions []Partition, number int) error {
	if err := validateAlignedRange(start, end, firstUsable, lastUsable); err != nil {
		return err
	}
	for _, other := range partitions {
		if other.Number != number && rangesOverlap(start, end, other.StartSector, other.EndSector) {
			return fmt.Errorf("resized partition %d would overlap partition %d", number, other.Number)
		}
	}
	return nil
}

func checkNewPartitionOverlap(number int, start, end uint64, partitions []Partition) error {
	for _, other := range partitions {
		if other.Number != number && rangesOverlap(start, end, other.StartSector, other.EndSector) {
			return fmt.Errorf("new partition would overlap partition %d", other.Number)
		}
	}
	return nil
}

func freeGPTSlot(state *gptState, requested int) (int, error) {
	if requested != 0 {
		if requested < 1 || requested > int(state.info.EntryCount) {
			return 0, fmt.Errorf("GPT partition number %d is out of range", requested)
		}
		entry := state.primaryEntries[(requested-1)*int(state.info.EntrySize) : requested*int(state.info.EntrySize)]
		if !zeroGUID([16]byte(entry[0:16])) {
			return 0, fmt.Errorf("GPT partition %d already exists", requested)
		}
		return requested, nil
	}
	for slot := 1; slot <= int(state.info.EntryCount); slot++ {
		entry := state.primaryEntries[(slot-1)*int(state.info.EntrySize) : slot*int(state.info.EntrySize)]
		if zeroGUID([16]byte(entry[0:16])) {
			return slot, nil
		}
	}
	return 0, errors.New("GPT entry array is full")
}

func findPartition(partitions []Partition, number int) (Partition, bool) {
	for _, partition := range partitions {
		if partition.Number == number {
			return partition, true
		}
	}
	return Partition{}, false
}

func writeMBR(f *os.File, raw []byte) error {
	n, err := f.WriteAt(raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return fmt.Errorf("short MBR write: wrote %d of %d bytes", n, len(raw))
	}
	return f.Sync()
}
