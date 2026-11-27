//go:build linux

package disk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateLinuxPartitionPaths(t *testing.T) {
	root := t.TempDir()
	diskSys := filepath.Join(root, "sda")
	partitionSys := filepath.Join(diskSys, "sda1")
	if err := os.MkdirAll(partitionSys, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(partitionSys, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("partition", "1\n")
	write("start", "2048\n")
	partition := Partition{Number: 1, StartSector: 2048, SectorSize: 512}
	if err := validateLinuxPartitionPaths(diskSys, partitionSys, partition); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}
	partition.SectorSize = 4096
	partition.StartSector = 256
	if err := validateLinuxPartitionPaths(diskSys, partitionSys, partition); err != nil {
		t.Fatalf("4 KiB partition rejected: %v", err)
	}
	partition.StartSector = 512
	if err := validateLinuxPartitionPaths(diskSys, partitionSys, partition); err == nil || !strings.Contains(err.Error(), "starts at") {
		t.Fatalf("wrong start error = %v", err)
	}
	if err := validateLinuxPartitionPaths(filepath.Join(root, "sdb"), partitionSys, partition); err == nil || !strings.Contains(err.Error(), "selected disk") {
		t.Fatalf("wrong parent error = %v", err)
	}
	write("partition", "2\n")
	if err := validateLinuxPartitionPaths(diskSys, partitionSys, partition); err == nil || !strings.Contains(err.Error(), "partition number") {
		t.Fatalf("wrong number error = %v", err)
	}
}
