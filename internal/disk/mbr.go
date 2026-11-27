package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const (
	mbrSignature = 0xaa55
	mbrEntryOff  = 446
)

func parseMBR(raw []byte, totalSectors uint64) (ProtectiveMBR, error) {
	if len(raw) < 512 {
		return ProtectiveMBR{}, errors.New("MBR is shorter than one sector")
	}
	if little16(raw[510:512]) != mbrSignature {
		return ProtectiveMBR{}, errors.New("invalid MBR signature")
	}
	mbr := ProtectiveMBR{Signature: mbrSignature}
	for slot := 0; slot < 4; slot++ {
		entry := raw[mbrEntryOff+slot*16 : mbrEntryOff+(slot+1)*16]
		if entry[4] == 0 {
			continue
		}
		if entry[0] != 0 && entry[0] != 0x80 {
			return ProtectiveMBR{}, fmt.Errorf("MBR partition %d has invalid status 0x%02x", slot+1, entry[0])
		}
		start := uint64(little32(entry[8:12]))
		count := uint64(little32(entry[12:16]))
		if start == 0 || count == 0 {
			return ProtectiveMBR{}, fmt.Errorf("MBR partition %d has an empty range", slot+1)
		}
		if count-1 > ^uint64(0)-start {
			return ProtectiveMBR{}, fmt.Errorf("MBR partition %d range overflows", slot+1)
		}
		end := start + count - 1
		if end >= totalSectors {
			return ProtectiveMBR{}, fmt.Errorf("MBR partition %d extends beyond the device", slot+1)
		}
		mbr.Partitions = append(mbr.Partitions, MBRPartition{
			Number:      slot + 1,
			Status:      entry[0],
			Type:        entry[4],
			StartLBA:    start,
			SectorCount: count,
			StartSector: start,
			EndSector:   end,
		})
	}
	if err := validatePartitionOverlap(mbr.Partitions); err != nil {
		return ProtectiveMBR{}, err
	}
	return mbr, nil
}

func validateProtectiveMBR(mbr ProtectiveMBR, totalSectors uint64) error {
	if mbr.Signature != mbrSignature || len(mbr.Partitions) != 1 {
		return errors.New("GPT requires exactly one protective MBR entry")
	}
	partition := mbr.Partitions[0]
	if partition.Status != 0 || partition.Type != 0xee || partition.StartLBA != 1 {
		return errors.New("invalid protective MBR entry")
	}
	wantSize := totalSectors - 1
	if wantSize > 0xffffffff {
		wantSize = 0xffffffff
	}
	if partition.SectorCount != wantSize {
		return fmt.Errorf("protective MBR covers %d sectors, want %d", partition.SectorCount, wantSize)
	}
	return nil
}

type sectorRange struct {
	number int
	start  uint64
	end    uint64
}

func validatePartitionOverlap(partitions []MBRPartition) error {
	ranges := make([]sectorRange, 0, len(partitions))
	for _, partition := range partitions {
		ranges = append(ranges, sectorRange{partition.Number, partition.StartSector, partition.EndSector})
	}
	return validateRanges(ranges)
}

func validateRanges(ranges []sectorRange) error {
	ordered := append([]sectorRange(nil), ranges...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].start == ordered[j].start {
			return ordered[i].end < ordered[j].end
		}
		return ordered[i].start < ordered[j].start
	})
	for i := 1; i < len(ordered); i++ {
		previous := ordered[i-1]
		current := ordered[i]
		if current.start <= previous.end {
			return fmt.Errorf("partitions %d and %d overlap", previous.number, current.number)
		}
	}
	return nil
}

func rangesOverlap(aStart, aEnd, bStart, bEnd uint64) bool {
	return aStart <= bEnd && bStart <= aEnd
}

func encodeCHS(lba uint64) [3]byte {
	const heads = uint64(255)
	const sectors = uint64(63)
	cylinder := lba / (heads * sectors)
	if cylinder > 1023 {
		return [3]byte{0xfe, 0xff, 0xff}
	}
	remainder := lba % (heads * sectors)
	head := remainder / sectors
	sector := remainder%sectors + 1
	return [3]byte{
		byte(head),
		byte(sector&0x3f) | byte((cylinder>>2)&0xc0),
		byte(cylinder & 0xff),
	}
}

func setMBRPartition(raw []byte, slot int, status, partitionType byte, start, count uint64) {
	entry := raw[mbrEntryOff+(slot-1)*16 : mbrEntryOff+slot*16]
	for i := range entry {
		entry[i] = 0
	}
	entry[0] = status
	startCHS := encodeCHS(start)
	copy(entry[1:4], startCHS[:])
	entry[4] = partitionType
	endCHS := encodeCHS(start + count - 1)
	copy(entry[5:8], endCHS[:])
	binary.LittleEndian.PutUint32(entry[8:12], uint32(start))
	binary.LittleEndian.PutUint32(entry[12:16], uint32(count))
}
