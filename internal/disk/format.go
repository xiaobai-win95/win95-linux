package disk

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// FormatPartition initializes an existing partition with ext4 or FAT. Only the
// exact partition device is passed to mkfs, as an absolute resolved path and a
// single argument (never through a shell). Formatting synthetic regular-file
// images is deliberately unsupported.
func FormatPartition(path string, number int, fs string, opts ...Option) error {
	filesystem := strings.ToLower(strings.TrimSpace(fs))
	var toolName string
	var args []string
	switch filesystem {
	case "ext4":
		toolName = "mkfs.ext4"
	case "fat", "vfat":
		toolName = "mkfs.fat"
	default:
		return fmt.Errorf("unsupported filesystem %q (want ext4 or fat)", fs)
	}
	if number <= 0 {
		return errors.New("partition number must be positive")
	}
	f, loaded, err := loadEditable(path, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !isBlockDevice(info.Mode()) {
		return errors.New("formatting a partition in a regular-file image is not supported")
	}
	partition, ok := findPartition(loaded.device.Partitions, number)
	if !ok {
		return fmt.Errorf("partition %d does not exist", number)
	}
	if partition.Table == TableMBR && (partition.Type == 0x05 || partition.Type == 0x0f || partition.Type == 0x85) {
		return errors.New("refusing to format an extended MBR container")
	}
	partitionPath, err := partitionDevicePath(loaded.device.Path, number)
	if err != nil {
		return err
	}
	partitionPath, err = resolvePath(partitionPath)
	if err != nil {
		return fmt.Errorf("resolve partition %d (the kernel may need to rescan the table): %w", number, err)
	}
	partitionInfo, err := validatePartitionNode(loaded.device.Path, partitionPath, partition.Bytes())
	if err != nil {
		return err
	}
	mounted, err := partitionMounted(partitionPath, partitionInfo)
	if err != nil {
		return fmt.Errorf("check partition mounts: %w", err)
	}
	if mounted {
		return fmt.Errorf("refusing to format mounted partition %s", partitionPath)
	}
	toolPath, err := exec.LookPath(toolName)
	if err != nil {
		return fmt.Errorf("find %s: %w", toolName, err)
	}
	toolPath, err = resolvePath(toolPath)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", toolName, err)
	}
	toolInfo, err := os.Stat(toolPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", toolName, err)
	}
	if !toolInfo.Mode().IsRegular() || toolInfo.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not an executable regular file", toolPath)
	}
	args = append(args, partitionPath)
	if filesystem == "ext4" {
		args = append([]string{"-F"}, args...)
	}
	command := exec.Command(toolPath, args...)
	var output limitedOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if message != "" {
			return fmt.Errorf("%s failed: %w: %s", toolName, err, message)
		}
		return fmt.Errorf("%s failed: %w", toolName, err)
	}
	return nil
}

func partitionDevicePath(diskPath string, number int) (string, error) {
	base := filepath.Base(diskPath)
	switch runtime.GOOS {
	case "linux":
		separator := ""
		for _, prefix := range []string{"nvme", "mmcblk", "loop", "md"} {
			if strings.HasPrefix(base, prefix) {
				separator = "p"
				break
			}
		}
		return diskPath + separator + strconv.Itoa(number), nil
	case "darwin":
		return diskPath + "s" + strconv.Itoa(number), nil
	default:
		return "", fmt.Errorf("partition device naming is unsupported on %s", runtime.GOOS)
	}
}

func validatePartitionNode(diskPath, partitionPath string, expectedBytes uint64) (os.FileInfo, error) {
	diskInfo, err := os.Stat(diskPath)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(partitionPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open partition %s for writing: %w", partitionPath, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !isBlockDevice(info.Mode()) {
		return nil, fmt.Errorf("partition path %s is not a block device", partitionPath)
	}
	if os.SameFile(diskInfo, info) {
		return nil, fmt.Errorf("refusing to format whole disk %s as a partition", partitionPath)
	}
	size, _, err := deviceGeometry(f, info.Mode())
	if err != nil {
		return nil, fmt.Errorf("get partition size: %w", err)
	}
	if size != expectedBytes {
		return nil, fmt.Errorf("partition %s has %d bytes, table expects %d", partitionPath, size, expectedBytes)
	}
	return info, nil
}

func partitionMounted(path string, info os.FileInfo) (bool, error) {
	if runtime.GOOS != "linux" {
		return false, nil
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false, fmt.Errorf("read mount table: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+2 >= len(fields) || !strings.HasPrefix(fields[separator+2], "/") {
			continue
		}
		source := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[separator+2])
		sourceInfo, err := os.Stat(source)
		if err == nil && os.SameFile(info, sourceInfo) {
			return true, nil
		}
	}
	return false, nil
}

type limitedOutput struct {
	buf []byte
}

func (output *limitedOutput) Write(data []byte) (int, error) {
	const limit = 4096
	written := len(data)
	if len(output.buf) < limit {
		remaining := limit - len(output.buf)
		if len(data) > remaining {
			data = data[:remaining]
		}
		output.buf = append(output.buf, data...)
	}
	return written, nil
}

func (output *limitedOutput) String() string {
	return string(output.buf)
}
