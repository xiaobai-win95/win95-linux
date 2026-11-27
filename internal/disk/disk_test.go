package disk

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const testDiskSectors = uint64(32768) // 16 MiB with 512-byte sectors.

type testPartition struct {
	number int
	name   string
	typeID string
	start  uint64
	end    uint64
	attrs  uint64
}

func newGPTImage(t *testing.T, partitions []testPartition) string {
	t.Helper()
	return newGPTImageSized(t, testDiskSectors, partitions)
}

func newGPTImageSized(t *testing.T, total uint64, partitions []testPartition) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gpt.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(int64(total * SectorSize)); err != nil {
		t.Fatal(err)
	}
	mbr := make([]byte, SectorSize)
	copy(mbr[:16], []byte("synthetic GPT MBR"))
	setMBRPartition(mbr, 1, 0, 0xee, 1, total-1)
	binary.LittleEndian.PutUint16(mbr[510:512], mbrSignature)
	writeTestAt(t, f, mbr, 0)

	entries := make([]byte, 128*gptEntrySize)
	for _, partition := range partitions {
		typeGUID, err := parseGUID(partition.typeID)
		if err != nil {
			t.Fatal(err)
		}
		uniqueGUID, err := randomGUID()
		if err != nil {
			t.Fatal(err)
		}
		if err := setGPTEntry(entries, partition.number, gptEntrySize, typeGUID, uniqueGUID, partition.start, partition.end, partition.attrs, partition.name); err != nil {
			t.Fatal(err)
		}
	}
	diskGUID, err := parseGUID("00112233-4455-6677-8899-AABBCCDDEEFF")
	if err != nil {
		t.Fatal(err)
	}
	entriesCRC := crc32.ChecksumIEEE(entries)
	backupEntriesLBA := total - 33
	makeHeader := func(current, backup, entriesLBA uint64) []byte {
		header := make([]byte, SectorSize)
		copy(header[:8], gptSignature)
		binary.LittleEndian.PutUint32(header[8:12], gptRevision)
		binary.LittleEndian.PutUint32(header[12:16], gptHeaderSize)
		binary.LittleEndian.PutUint64(header[24:32], current)
		binary.LittleEndian.PutUint64(header[32:40], backup)
		binary.LittleEndian.PutUint64(header[40:48], 34)
		binary.LittleEndian.PutUint64(header[48:56], total-34)
		copy(header[56:72], diskGUID[:])
		binary.LittleEndian.PutUint64(header[72:80], entriesLBA)
		binary.LittleEndian.PutUint32(header[80:84], 128)
		binary.LittleEndian.PutUint32(header[84:88], gptEntrySize)
		binary.LittleEndian.PutUint32(header[88:92], entriesCRC)
		binary.LittleEndian.PutUint32(header[16:20], crc32.ChecksumIEEE(header[:gptHeaderSize]))
		return header
	}
	writeTestAt(t, f, entries, int64(2*SectorSize))
	writeTestAt(t, f, entries, int64(backupEntriesLBA*SectorSize))
	writeTestAt(t, f, makeHeader(1, total-1, 2), int64(SectorSize))
	writeTestAt(t, f, makeHeader(total-1, 1, backupEntriesLBA), int64((total-1)*SectorSize))
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	return path
}

func newMBRImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mbr.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(int64(testDiskSectors * SectorSize)); err != nil {
		t.Fatal(err)
	}
	mbr := make([]byte, SectorSize)
	for i := 0; i < 440; i++ {
		mbr[i] = byte(i%251 + 1)
	}
	setMBRPartition(mbr, 1, 0x80, 0x0c, 2048, 4096)
	setMBRPartition(mbr, 3, 0, 0x83, 8192, 2048)
	binary.LittleEndian.PutUint16(mbr[510:512], mbrSignature)
	writeTestAt(t, f, mbr, 0)
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestAt(t *testing.T, f *os.File, data []byte, offset int64) {
	t.Helper()
	n, err := f.WriteAt(data, offset)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("short write: %d of %d", n, len(data))
	}
}

func readTestAt(t *testing.T, path string, offset int64, length int) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := make([]byte, length)
	n, err := f.ReadAt(data, offset)
	if err != nil {
		t.Fatal(err)
	}
	if n != length {
		t.Fatalf("short read: %d of %d", n, length)
	}
	return data
}

func inspectTest(t *testing.T, path string) *Device {
	t.Helper()
	device, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func partitionTest(t *testing.T, device *Device, number int) Partition {
	t.Helper()
	partition, ok := findPartition(device.Partitions, number)
	if !ok {
		t.Fatalf("partition %d not found in %+v", number, device.Partitions)
	}
	return partition
}

func assertValidGPTCopies(t *testing.T, path string) *Device {
	t.Helper()
	device := inspectTest(t, path)
	if device.GPT == nil {
		t.Fatal("missing GPT metadata")
	}
	primaryEntries := readTestAt(t, path, int64(device.GPT.PrimaryEntriesLBA*SectorSize), int(device.GPT.EntryCount*device.GPT.EntrySize))
	backupEntries := readTestAt(t, path, int64(device.GPT.BackupEntriesLBA*SectorSize), len(primaryEntries))
	if !bytes.Equal(primaryEntries, backupEntries) {
		t.Fatal("primary and backup entries differ")
	}
	if got := crc32.ChecksumIEEE(primaryEntries); got != device.GPT.PrimaryEntriesCRC32 || got != device.GPT.BackupEntriesCRC32 {
		t.Fatalf("entry CRC32 = %08x, primary=%08x backup=%08x", got, device.GPT.PrimaryEntriesCRC32, device.GPT.BackupEntriesCRC32)
	}
	for _, location := range []struct {
		lba  uint64
		name string
	}{{device.GPT.PrimaryHeaderLBA, "primary"}, {device.GPT.BackupHeaderLBA, "backup"}} {
		header := readTestAt(t, path, int64(location.lba*SectorSize), SectorSize)
		if !validTestHeaderCRC(header) {
			t.Fatalf("%s header CRC32 is invalid", location.name)
		}
		if got := little32(header[88:92]); got != crc32.ChecksumIEEE(primaryEntries) {
			t.Fatalf("%s header entry CRC32 = %08x", location.name, got)
		}
	}
	return device
}

func validTestHeaderCRC(header []byte) bool {
	size := little32(header[12:16])
	if size < gptHeaderSize || size > uint32(len(header)) {
		return false
	}
	checksum := append([]byte(nil), header[:size]...)
	for i := 16; i < 20; i++ {
		checksum[i] = 0
	}
	return crc32.ChecksumIEEE(checksum) == little32(header[16:20])
}

func TestGPTRoundtripCreateDeleteResizeBackupRestore(t *testing.T) {
	path := newGPTImage(t, []testPartition{
		{number: 1, name: "WIN95", typeID: TypeESP, start: 2048, end: 6143},
		{number: 2, name: "DATA", typeID: TypeLinuxData, start: 8192, end: 12287},
	})
	initial := inspectTest(t, path)
	if initial.Table != TableGPT || initial.TotalSectors != testDiskSectors || len(initial.Partitions) != 2 {
		t.Fatalf("unexpected device: %+v", initial)
	}
	if initial.GPT.PrimaryHeaderLBA != 1 || initial.GPT.BackupHeaderLBA != testDiskSectors-1 || initial.GPT.PrimaryEntriesLBA != 2 || initial.GPT.BackupEntriesLBA != testDiskSectors-33 {
		t.Fatalf("unexpected GPT locations: %+v", initial.GPT)
	}
	if len(initial.MBR.Partitions) != 1 || initial.MBR.Partitions[0].Type != 0xee {
		t.Fatalf("invalid protective MBR: %+v", initial.MBR)
	}
	rawMBR := readTestAt(t, path, 0, SectorSize)
	marker := []byte("partition data must survive metadata restore")
	writeTestAt(t, mustOpenTest(t, path), marker, int64(4096*SectorSize))

	backupPath := filepath.Join(t.TempDir(), "table.backup.json")
	backup, err := BackupTable(path, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Version != backupVersion || !bytes.Equal(backup.RawMBR, rawMBR) {
		t.Fatalf("unexpected backup: %+v", backup)
	}
	if data, err := os.ReadFile(backupPath); err != nil || !json.Valid(data) {
		t.Fatalf("backup file is not valid JSON: %v", err)
	}

	err = CreatePartition(path, PartitionSpec{
		Name:        "新分区",
		TypeGUID:    TypeLinuxData,
		StartSector: 16384,
		SizeSectors: 2048,
		Attributes:  0x1234,
	}, AllowRegularFile())
	if err != nil {
		t.Fatal(err)
	}
	device := assertValidGPTCopies(t, path)
	created := partitionTest(t, device, 3)
	if created.Name != "新分区" || created.StartSector != 16384 || created.EndSector != 18431 || created.Sectors() != 2048 || created.Bytes() != 1<<20 || created.Attributes != 0x1234 {
		t.Fatalf("unexpected created partition: %+v", created)
	}
	if created.UniqueGUID == "" || created.UniqueGUID == initial.Partitions[0].UniqueGUID {
		t.Fatalf("invalid unique GUID %q", created.UniqueGUID)
	}
	if got := readTestAt(t, path, 0, SectorSize); !bytes.Equal(got, rawMBR) {
		t.Fatal("create changed the protective MBR")
	}

	if err := ResizePartition(path, 3, 24575, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	device = assertValidGPTCopies(t, path)
	if got := partitionTest(t, device, 3); got.EndSector != 24575 || got.Sectors() != 8192 {
		t.Fatalf("unexpected resized partition: %+v", got)
	}
	if err := ResizePartition(path, 1, 10239, AllowRegularFile()); err == nil {
		t.Fatal("overlapping resize succeeded")
	}
	if err := DeletePartition(path, 2, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	device = assertValidGPTCopies(t, path)
	if len(device.Partitions) != 2 || device.Partitions[0].Number != 1 || device.Partitions[1].Number != 3 {
		t.Fatalf("delete renumbered or removed the wrong entries: %+v", device.Partitions)
	}

	beforeRejected := readTestAt(t, path, int64(device.GPT.PrimaryEntriesLBA*SectorSize), int(device.GPT.EntryCount*device.GPT.EntrySize))
	if err := CreatePartition(path, PartitionSpec{Name: "overlap", TypeGUID: TypeLinuxData, StartSector: 22528, SizeSectors: 2048}, AllowRegularFile()); err == nil {
		t.Fatal("overlapping create succeeded")
	}
	if err := CreatePartition(path, PartitionSpec{Name: "unaligned", TypeGUID: TypeLinuxData, StartSector: 2049, SizeSectors: 2048}, AllowRegularFile()); err == nil {
		t.Fatal("unaligned create succeeded")
	}
	if after := readTestAt(t, path, int64(device.GPT.PrimaryEntriesLBA*SectorSize), len(beforeRejected)); !bytes.Equal(after, beforeRejected) {
		t.Fatal("rejected edit changed the entry array")
	}

	if err := RestoreTable(path, backup, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	restored := assertValidGPTCopies(t, path)
	if !reflect.DeepEqual(restored.Partitions, initial.Partitions) {
		t.Fatalf("restored partitions = %+v, want %+v", restored.Partitions, initial.Partitions)
	}
	if restored.GPT.DiskGUID != initial.GPT.DiskGUID {
		t.Fatal("restore changed the disk GUID")
	}
	if got := readTestAt(t, path, 0, SectorSize); !bytes.Equal(got, rawMBR) {
		t.Fatal("restore changed the protective MBR")
	}
	if got := readTestAt(t, path, int64(4096*SectorSize), len(marker)); !bytes.Equal(got, marker) {
		t.Fatal("metadata restore changed partition data")
	}
}

func mustOpenTest(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

type fakeGPTIO struct {
	data         []byte
	operations   []string
	writeCalls   int
	syncCalls    int
	failWrite    int
	failSync     int
	shortWrite   bool
	failRollback bool
}

func (f *fakeGPTIO) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset > int64(len(f.data)) {
		return 0, errors.New("read outside test image")
	}
	n := copy(p, f.data[offset:])
	if n != len(p) {
		return n, errors.New("short test read")
	}
	return n, nil
}

func (f *fakeGPTIO) WriteAt(p []byte, offset int64) (int, error) {
	f.writeCalls++
	f.operations = append(f.operations, "write")
	if f.writeCalls == f.failWrite {
		if f.shortWrite {
			n := len(p) / 2
			copy(f.data[offset:], p[:n])
			return n, nil
		}
		return 0, errors.New("injected GPT write failure")
	}
	if f.failRollback && f.writeCalls > 2 {
		return 0, errors.New("injected GPT rollback failure")
	}
	if offset < 0 || offset > int64(len(f.data)) || len(p) > len(f.data)-int(offset) {
		return 0, errors.New("write outside test image")
	}
	copy(f.data[offset:], p)
	return len(p), nil
}

func (f *fakeGPTIO) Sync() error {
	f.syncCalls++
	f.operations = append(f.operations, "sync")
	if f.syncCalls == f.failSync {
		return errors.New("injected GPT sync failure")
	}
	return nil
}

func TestGPTWriteSyncsStagesAndRollsBackFailures(t *testing.T) {
	path := newGPTImageSized(t, 8192, []testPartition{{number: 1, name: "DATA", typeID: TypeLinuxData, start: 2048, end: 4095}})
	loaded, err := loadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(loaded.gpt.primaryEntries[40:48], 6143)
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("success syncs every metadata object", func(t *testing.T) {
		fake := &fakeGPTIO{data: append([]byte(nil), image...)}
		if err := loaded.gpt.write(fake); err != nil {
			t.Fatal(err)
		}
		want := []string{"write", "sync", "write", "sync", "write", "sync", "write", "sync"}
		if !reflect.DeepEqual(fake.operations, want) {
			t.Fatalf("operations = %v, want %v", fake.operations, want)
		}
		primaryEntries := fake.data[int64(loaded.gpt.info.PrimaryEntriesLBA)*SectorSize:][:len(loaded.gpt.primaryEntries)]
		backupEntries := fake.data[int64(loaded.gpt.info.BackupEntriesLBA)*SectorSize:][:len(loaded.gpt.backupEntries)]
		if !bytes.Equal(primaryEntries, backupEntries) || !bytes.Equal(primaryEntries, loaded.gpt.primaryEntries) {
			t.Fatal("committed GPT entry arrays differ")
		}
	})

	for _, mode := range []string{"write", "sync"} {
		for stage := 1; stage <= 4; stage++ {
			t.Run(mode+" stage "+string(rune('0'+stage)), func(t *testing.T) {
				fake := &fakeGPTIO{data: append([]byte(nil), image...)}
				if mode == "write" {
					fake.failWrite = stage
				} else {
					fake.failSync = stage
				}
				if err := loaded.gpt.write(fake); err == nil {
					t.Fatal("GPT failure was silently ignored")
				}
				if !bytes.Equal(fake.data, image) {
					t.Fatal("GPT metadata was not rolled back exactly")
				}
			})
		}
	}

	t.Run("short write rolls back", func(t *testing.T) {
		fake := &fakeGPTIO{data: append([]byte(nil), image...), failWrite: 1, shortWrite: true}
		if err := loaded.gpt.write(fake); err == nil || !strings.Contains(err.Error(), "short write") {
			t.Fatalf("error = %v, want short write", err)
		}
		if !bytes.Equal(fake.data, image) {
			t.Fatal("short write did not restore the previous GPT metadata")
		}
	})

	t.Run("rollback failure is reported", func(t *testing.T) {
		fake := &fakeGPTIO{data: append([]byte(nil), image...), failWrite: 2, failRollback: true}
		err := loaded.gpt.write(fake)
		if err == nil || !strings.Contains(err.Error(), "rollback GPT metadata") {
			t.Fatalf("error = %v, want rollback failure", err)
		}
	})
}

func TestMBRInspectEditBackupRestore(t *testing.T) {
	path := newMBRImage(t)
	initial := inspectTest(t, path)
	if initial.Table != TableMBR || initial.GPT != nil || len(initial.Partitions) != 2 {
		t.Fatalf("unexpected MBR device: %+v", initial)
	}
	first := partitionTest(t, initial, 1)
	third := partitionTest(t, initial, 3)
	if first.Status != 0x80 || first.Type != 0x0c || first.StartSector != 2048 || first.EndSector != 6143 {
		t.Fatalf("unexpected MBR partition 1: %+v", first)
	}
	if third.Number != 3 || third.Type != 0x83 || third.StartSector != 8192 || third.EndSector != 10239 {
		t.Fatalf("unexpected MBR partition 3: %+v", third)
	}
	rawMBR := readTestAt(t, path, 0, SectorSize)
	backup, err := BackupTable(path, filepath.Join(t.TempDir(), "mbr.backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CreatePartition(path, PartitionSpec{Number: 2, Type: 0x83, StartSector: 12288, SizeSectors: 2048}, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	device := inspectTest(t, path)
	if got := partitionTest(t, device, 2); got.EndSector != 14335 || got.Type != 0x83 {
		t.Fatalf("unexpected created MBR partition: %+v", got)
	}
	if err := ResizePartition(path, 2, 16383, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	if got := partitionTest(t, inspectTest(t, path), 2); got.EndSector != 16383 || got.Sectors() != 4096 {
		t.Fatalf("unexpected resized MBR partition: %+v", got)
	}
	if err := CreatePartition(path, PartitionSpec{Number: 4, Type: 0x83, StartSector: 14336, SizeSectors: 2048}, AllowRegularFile()); err == nil {
		t.Fatal("overlapping MBR create succeeded")
	}
	if err := DeletePartition(path, 1, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	if _, ok := findPartition(inspectTest(t, path).Partitions, 1); ok {
		t.Fatal("MBR partition 1 was not deleted")
	}
	if err := RestoreTable(path, backup, AllowRegularFile()); err != nil {
		t.Fatal(err)
	}
	restored := inspectTest(t, path)
	if !reflect.DeepEqual(restored.Partitions, initial.Partitions) {
		t.Fatalf("restored MBR partitions = %+v, want %+v", restored.Partitions, initial.Partitions)
	}
	if got := readTestAt(t, path, 0, SectorSize); !bytes.Equal(got, rawMBR) {
		t.Fatal("MBR restore did not reproduce the original sector")
	}
}

func TestRestoreRejectsDifferentPartitionTableWithoutWrites(t *testing.T) {
	gptPath := newGPTImage(t, nil)
	mbrPath := newMBRImage(t)
	gptBackup, err := BackupTable(gptPath, filepath.Join(t.TempDir(), "gpt.json"))
	if err != nil {
		t.Fatal(err)
	}
	mbrBackup, err := BackupTable(mbrPath, filepath.Join(t.TempDir(), "mbr.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := func(path string) []byte {
		data := readTestAt(t, path, 0, 34*SectorSize)
		return append(data, readTestAt(t, path, int64((testDiskSectors-33)*SectorSize), 33*SectorSize)...)
	}
	beforeGPT := metadata(gptPath)
	if err := RestoreTable(gptPath, mbrBackup, AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "cannot restore mbr") {
		t.Fatalf("MBR-to-GPT restore error = %v", err)
	}
	if after := metadata(gptPath); !bytes.Equal(after, beforeGPT) {
		t.Fatal("rejected MBR-to-GPT restore changed GPT metadata")
	}
	beforeMBR := metadata(mbrPath)
	if err := RestoreTable(mbrPath, gptBackup, AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "cannot restore gpt") {
		t.Fatalf("GPT-to-MBR restore error = %v", err)
	}
	if after := metadata(mbrPath); !bytes.Equal(after, beforeMBR) {
		t.Fatal("rejected GPT-to-MBR restore changed destination metadata")
	}
	damagedGPT := newGPTImage(t, nil)
	writeTestAt(t, mustOpenTest(t, damagedGPT), make([]byte, 34*SectorSize), 0)
	beforeDamaged := metadata(damagedGPT)
	if err := RestoreTable(damagedGPT, mbrBackup, AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "cannot restore mbr") {
		t.Fatalf("MBR restore with only backup GPT remaining error = %v", err)
	}
	if after := metadata(damagedGPT); !bytes.Equal(after, beforeDamaged) {
		t.Fatal("rejected restore changed surviving backup GPT metadata")
	}
}

func TestEditsRejectChangedDeviceIdentity(t *testing.T) {
	path := newGPTImage(t, nil)
	before := readTestAt(t, path, 0, 34*SectorSize)
	err := CreatePartition(path, PartitionSpec{TypeGUID: TypeLinuxData, StartSector: 2048, SizeSectors: 2048}, Options{
		AllowRegularFile: true,
		ExpectedDeviceID: "8:16",
	})
	if err == nil || !strings.Contains(err.Error(), "device changed after validation") {
		t.Fatalf("expected-device error = %v", err)
	}
	if after := readTestAt(t, path, 0, 34*SectorSize); !bytes.Equal(after, before) {
		t.Fatal("rejected device identity changed the image")
	}
	backup, err := BackupTable(path, filepath.Join(t.TempDir(), "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = RestoreTable(path, backup, Options{AllowRegularFile: true, ExpectedDeviceID: "8:16"})
	if err == nil || !strings.Contains(err.Error(), "device changed after validation") {
		t.Fatalf("restore expected-device error = %v", err)
	}
	if after := readTestAt(t, path, 0, 34*SectorSize); !bytes.Equal(after, before) {
		t.Fatal("identity-rejected restore changed the image")
	}
}

func TestEditsRequireExplicitRegularFileOption(t *testing.T) {
	path := newGPTImage(t, []testPartition{{number: 1, name: "DATA", typeID: TypeLinuxData, start: 2048, end: 6143}})
	backup, err := BackupTable(path, filepath.Join(t.TempDir(), "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := append(readTestAt(t, path, 0, 34*SectorSize), readTestAt(t, path, int64((testDiskSectors-33)*SectorSize), 33*SectorSize)...)
	operations := []struct {
		name string
		run  func() error
	}{
		{"create", func() error {
			return CreatePartition(path, PartitionSpec{TypeGUID: TypeLinuxData, StartSector: 8192, SizeSectors: 2048})
		}},
		{"delete", func() error { return DeletePartition(path, 1) }},
		{"resize", func() error { return ResizePartition(path, 1, 8191) }},
		{"restore", func() error { return RestoreTable(path, backup) }},
		{"format", func() error { return FormatPartition(path, 1, "ext4") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("error = %v, want regular-file guard", err)
			}
		})
	}
	after := append(readTestAt(t, path, 0, 34*SectorSize), readTestAt(t, path, int64((testDiskSectors-33)*SectorSize), 33*SectorSize)...)
	if !bytes.Equal(before, after) {
		t.Fatal("a rejected regular-file edit changed the image")
	}
	if err := CreatePartition(path, PartitionSpec{Number: 4, TypeGUID: TypeLinuxData, StartSector: 16384, SizeSectors: 2048}, Options{AllowRegularFile: true}); err != nil {
		t.Fatalf("Options regular-file escape hatch failed: %v", err)
	}
}

func TestInspectRejectsCorruptGPTAndMBR(t *testing.T) {
	t.Run("primary header CRC", func(t *testing.T) {
		path := newGPTImage(t, nil)
		f := mustOpenTest(t, path)
		header := readTestAt(t, path, SectorSize, SectorSize)
		header[20] ^= 1
		writeTestAt(t, f, header, SectorSize)
		if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "CRC32") {
			t.Fatalf("error = %v, want header CRC failure", err)
		}
	})
	t.Run("backup entries CRC", func(t *testing.T) {
		path := newGPTImage(t, nil)
		f := mustOpenTest(t, path)
		offset := int64((testDiskSectors - 33) * SectorSize)
		entries := readTestAt(t, path, offset, gptEntrySize)
		entries[0] ^= 1
		writeTestAt(t, f, entries, offset)
		if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "backup GPT entries CRC32") {
			t.Fatalf("error = %v, want backup entry CRC failure", err)
		}
	})
	t.Run("protective MBR", func(t *testing.T) {
		path := newGPTImage(t, nil)
		f := mustOpenTest(t, path)
		mbr := readTestAt(t, path, 0, SectorSize)
		mbr[mbrEntryOff+4] = 0x83
		writeTestAt(t, f, mbr, 0)
		if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "protective MBR") {
			t.Fatalf("error = %v, want protective MBR failure", err)
		}
	})
	t.Run("MBR signature", func(t *testing.T) {
		path := newMBRImage(t)
		f := mustOpenTest(t, path)
		mbr := readTestAt(t, path, 0, SectorSize)
		mbr[510] = 0
		writeTestAt(t, f, mbr, 0)
		if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "signature") {
			t.Fatalf("error = %v, want MBR signature failure", err)
		}
	})
}

func TestBackupValidationAndGeometry(t *testing.T) {
	path := newGPTImage(t, []testPartition{{number: 1, name: "DATA", typeID: TypeLinuxData, start: 2048, end: 6143}})
	backup, err := BackupTable(path, filepath.Join(t.TempDir(), "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BackupTable(path, path); err == nil {
		t.Fatal("backup destination equal to source was accepted")
	}
	before := readTestAt(t, path, SectorSize, SectorSize)
	corrupt := backup
	corrupt.PrimaryEntries = append([]byte(nil), backup.PrimaryEntries...)
	corrupt.PrimaryEntries[0] ^= 1
	if err := RestoreTable(path, corrupt, AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "CRC32") {
		t.Fatalf("error = %v, want backup CRC failure", err)
	}
	if after := readTestAt(t, path, SectorSize, SectorSize); !bytes.Equal(before, after) {
		t.Fatal("invalid backup changed the destination")
	}
	other := newGPTImageSized(t, testDiskSectors+2048, nil)
	if err := RestoreTable(other, backup, AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "sectors") {
		t.Fatalf("error = %v, want geometry mismatch", err)
	}
	blank := filepath.Join(t.TempDir(), "blank.img")
	f, err := os.Create(blank)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(testDiskSectors * SectorSize)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RestoreTable(blank, backup, AllowRegularFile()); err != nil {
		t.Fatalf("restore to blank same-sized image failed: %v", err)
	}
	if restored := assertValidGPTCopies(t, blank); !reflect.DeepEqual(restored.Partitions, backup.Device.Partitions) {
		t.Fatalf("blank-image restore partitions = %+v, want %+v", restored.Partitions, backup.Device.Partitions)
	}
}

func TestCreateValidationDoesNotWrite(t *testing.T) {
	path := newGPTImage(t, nil)
	before := readTestAt(t, path, 0, 34*SectorSize)
	tests := []PartitionSpec{
		{Name: "missing range", TypeGUID: TypeLinuxData},
		{Name: "outside", TypeGUID: TypeLinuxData, StartSector: 30720, SizeSectors: 4096},
		{Name: "bad end", TypeGUID: TypeLinuxData, StartSector: 2048, EndSector: 6144},
		{Name: "bad GUID", TypeGUID: "not-a-guid", StartSector: 2048, SizeSectors: 2048},
		{Name: strings.Repeat("x", maxGPTNameUTF16+1), TypeGUID: TypeLinuxData, StartSector: 2048, SizeSectors: 2048},
		{Name: "mismatched size", TypeGUID: TypeLinuxData, StartSector: 2048, EndSector: 4095, SizeSectors: 4096},
	}
	for _, spec := range tests {
		if err := CreatePartition(path, spec, AllowRegularFile()); err == nil {
			t.Fatalf("invalid spec %+v was accepted", spec)
		}
	}
	if after := readTestAt(t, path, 0, 34*SectorSize); !bytes.Equal(before, after) {
		t.Fatal("invalid create changed the primary metadata")
	}
}

func TestFormatPartitionValidationAndDeviceNaming(t *testing.T) {
	path := newGPTImage(t, []testPartition{{number: 1, name: "DATA", typeID: TypeLinuxData, start: 2048, end: 6143}})
	if err := FormatPartition(path, 1, "ext4; touch /tmp/not-allowed"); err == nil {
		t.Fatal("unvalidated filesystem string was accepted")
	}
	if err := FormatPartition(path, 1, "fat", AllowRegularFile()); err == nil || !strings.Contains(err.Error(), "regular-file image") {
		t.Fatalf("error = %v, want synthetic-image formatting guard", err)
	}
	if got := readTestAt(t, path, 0, SectorSize); little16(got[510:512]) != mbrSignature {
		t.Fatal("format validation changed the image")
	}
	var diskPath, want string
	switch runtime.GOOS {
	case "linux":
		diskPath, want = "/dev/nvme0n1", "/dev/nvme0n1p2"
	case "darwin":
		diskPath, want = "/dev/disk0", "/dev/disk0s2"
	}
	if want != "" {
		got, err := partitionDevicePath(diskPath, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("partition path = %q, want %q", got, want)
		}
	}
}
