package gpt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const total = uint64(8 << 20)
	if err := f.Truncate(int64(total * SectorSize)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	parts := []NewPartition{
		{Name: "WIN95INST", TypeGUID: TypeESP, FirstLBA: 2048, LastLBA: 2048 + 4096 - 1},
		{Name: "WIN95ROOT", TypeGUID: TypeLinuxData, FirstLBA: 8192, LastLBA: total - 34},
	}
	written, err := Write(path, total, parts)
	if err != nil {
		t.Fatal(err)
	}
	read, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if read.TotalSectors != total {
		t.Fatalf("total sectors = %d, want %d", read.TotalSectors, total)
	}
	if len(read.Partitions) != len(parts) {
		t.Fatalf("partitions = %d, want %d", len(read.Partitions), len(parts))
	}
	for i, want := range written.Partitions {
		got := read.Partitions[i]
		if got.Name != want.Name || got.FirstLBA != want.FirstLBA || got.LastLBA != want.LastLBA {
			t.Fatalf("partition %d = %+v, want %+v", i, got, want)
		}
		if got.UniqueGUID != want.UniqueGUID {
			t.Fatalf("partition %d UUID = %s, want %s", i, got.UniqueGUID, want.UniqueGUID)
		}
	}
	if p, ok := FindByLabel(read, "WIN95INST"); !ok || p.Index != 1 {
		t.Fatalf("FindByLabel = %+v, %v", p, ok)
	}
}
