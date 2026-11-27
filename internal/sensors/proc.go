package sensors

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

type cpuTimes struct {
	values [8]uint64 // user, nice, system, idle, iowait, irq, softirq, steal
}

func readCPUTimes(path string) (cpuTimes, bool, error) {
	var times cpuTimes
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return times, false, nil
		}
		return times, false, wrapPathError(path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		if len(fields) < 5 {
			return times, false, fmt.Errorf("sensors: parse %s: short CPU counters", path)
		}
		// guest and guest_nice are already included in user and nice, so only
		// the first eight counters are summed.
		for i := 1; i < len(fields) && i <= len(times.values); i++ {
			value, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return times, false, fmt.Errorf("sensors: parse %s: CPU counter %d: %w", path, i, err)
			}
			times.values[i-1] = value
		}
		return times, true, nil
	}
	if err := scanner.Err(); err != nil {
		return times, false, wrapPathError(path, err)
	}
	return times, false, fmt.Errorf("sensors: parse %s: missing aggregate CPU counters", path)
}

func (current cpuTimes) utilizationSince(previous cpuTimes) (float64, bool) {
	var totalDelta, idleDelta uint64
	for i, value := range current.values {
		if value < previous.values[i] {
			return 0, false
		}
		delta := value - previous.values[i]
		totalDelta += delta
		if i == 3 || i == 4 { // idle and iowait
			idleDelta += delta
		}
	}
	if totalDelta == 0 {
		return 0, false
	}
	busyDelta := totalDelta - idleDelta
	return float64(busyDelta) * 100 / float64(totalDelta), true
}

func readMemory(path string) (MemoryUsage, error) {
	memory := MemoryUsage{
		Label:       "Memory",
		Unit:        UnitBytes,
		PercentUnit: UnitPercent,
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return memory, nil
		}
		return memory, wrapPathError(path, err)
	}
	defer file.Close()

	var total, available, free, buffers, cached, reclaimable, shmem uint64
	var hasTotal, hasAvailable, hasFree bool
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || (len(fields) >= 3 && fields[2] != "kB") {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total, hasTotal = value, true
		case "MemAvailable":
			available, hasAvailable = value, true
		case "MemFree":
			free, hasFree = value, true
		case "Buffers":
			buffers = value
		case "Cached":
			cached = value
		case "SReclaimable":
			reclaimable = value
		case "Shmem":
			shmem = value
		}
	}
	if err := scanner.Err(); err != nil {
		return memory, wrapPathError(path, err)
	}
	if !hasTotal || total == 0 || total > ^uint64(0)/1024 {
		return memory, nil
	}
	memory.TotalBytes = total * 1024

	if !hasAvailable && hasFree {
		available = saturatingAdd(free, buffers)
		available = saturatingAdd(available, cached)
		available = saturatingAdd(available, reclaimable)
		if shmem <= available {
			available -= shmem
		}
		hasAvailable = true
	}
	if !hasAvailable {
		return memory, nil
	}
	if available > total {
		available = total
	}
	memory.AvailableBytes = available * 1024
	memory.UsedBytes = memory.TotalBytes - memory.AvailableBytes
	memory.UsedPercent = float64(memory.UsedBytes) * 100 / float64(memory.TotalBytes)
	memory.Available = true
	return memory, nil
}

func saturatingAdd(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}
