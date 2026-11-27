// Package disk provides an in-process MBR/GPT partition-table engine.
//
// All offsets and lengths are expressed in the device's logical sectors. Edit
// operations require a writable block device. Regular files are accepted only
// when the explicit AllowRegularFile test option is supplied.
package disk

const (
	// SectorSize is the default logical sector size.
	SectorSize = 512
	// AlignmentSectors is the required partition boundary alignment.
	AlignmentSectors = 2048

	TableGPT = "gpt"
	TableMBR = "mbr"

	TypeESP         = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	TypeLinuxData   = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
	TypeLinuxFS     = TypeLinuxData
	backupVersion   = 1
	gptHeaderSize   = 92
	gptEntrySize    = 128
	maxGPTNameUTF16 = 36
)

// Device is an inspected disk. Partitions retain their table slot numbers;
// deleting a partition does not renumber the remaining partitions.
type Device struct {
	Path         string
	DeviceID     string `json:"-"` // Runtime major:minor identity; never persisted in table backups.
	Size         uint64
	SectorSize   uint32
	TotalSectors uint64
	Table        string
	MBR          ProtectiveMBR
	Partitions   []Partition
	GPT          *GPT
}

// GPT describes both copies of a GUID partition table.
type GPT struct {
	Revision            uint32
	HeaderSize          uint32
	DiskGUID            string
	PrimaryHeaderLBA    uint64
	BackupHeaderLBA     uint64
	FirstUsableLBA      uint64
	LastUsableLBA       uint64
	PrimaryEntriesLBA   uint64
	BackupEntriesLBA    uint64
	EntryCount          uint32
	EntrySize           uint32
	PrimaryEntriesCRC32 uint32
	BackupEntriesCRC32  uint32
}

// Partition is a GPT entry or a primary MBR entry. GPT partitions have a
// TypeGUID and UniqueGUID; MBR partitions have a Type byte.
type Partition struct {
	Number      int
	Table       string
	Name        string
	TypeGUID    string
	UniqueGUID  string
	Type        byte
	Status      uint8 // MBR only.
	StartSector uint64
	EndSector   uint64
	Attributes  uint64
	SectorSize  uint32
}

// Sectors returns the inclusive sector count.
func (p Partition) Sectors() uint64 {
	if p.EndSector < p.StartSector {
		return 0
	}
	return p.EndSector - p.StartSector + 1
}

// Bytes returns the partition size in bytes.
func (p Partition) Bytes() uint64 {
	sectorSize := p.SectorSize
	if sectorSize == 0 {
		sectorSize = SectorSize
	}
	return p.Sectors() * uint64(sectorSize)
}

// PartitionSpec describes a new primary partition. EndSector is inclusive.
// SizeSectors may be used instead of EndSector. Number optionally selects a
// table slot; zero selects the first free slot. Starts and ends must already be
// aligned to AlignmentSectors.
type PartitionSpec struct {
	Number      int
	Name        string
	TypeGUID    string
	Type        byte // MBR only; zero defaults to Linux (0x83).
	StartSector uint64
	EndSector   uint64
	SizeSectors uint64
	Attributes  uint64
}

// MBRPartition is one of the four primary MBR slots.
type MBRPartition struct {
	Number      int
	Status      uint8
	Type        byte
	StartLBA    uint64
	SectorCount uint64
	StartSector uint64
	EndSector   uint64
}

// ProtectiveMBR is the parsed sector-zero MBR. For GPT disks it contains the
// single protective 0xEE entry; for MBR disks it contains primary entries.
type ProtectiveMBR struct {
	Signature  uint16
	Partitions []MBRPartition
}

// Backup contains a validated snapshot of the partition metadata and the raw
// sectors needed to restore it. It can be serialized as JSON; BackupTable uses
// that representation for dest. Partition contents are never included.
type Backup struct {
	Version        int    `json:"version"`
	SourcePath     string `json:"source_path"`
	Device         Device `json:"device"`
	RawMBR         []byte `json:"raw_mbr"`
	PrimaryHeader  []byte `json:"primary_header,omitempty"`
	PrimaryEntries []byte `json:"primary_entries,omitempty"`
	BackupHeader   []byte `json:"backup_header,omitempty"`
	BackupEntries  []byte `json:"backup_entries,omitempty"`
}
