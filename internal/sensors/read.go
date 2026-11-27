package sensors

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

const (
	defaultSysRoot  = "/sys"
	defaultProcRoot = "/proc"
)

// Read takes a single sensor snapshot. CPU utilization needs two /proc/stat
// samples, so this convenience function reports it as unavailable. Use a
// Sampler and call its Read method repeatedly when utilization is required.
// Empty roots default to /sys and /proc.
func Read(sysRoot, procRoot string) (Snapshot, error) {
	return NewSampler().Read(sysRoot, procRoot)
}

// Sampler retains the previous aggregate CPU counters. Its zero value is ready
// for use, and its methods are safe for concurrent calls.
type Sampler struct {
	mu          sync.Mutex
	procRoot    string
	previous    cpuTimes
	hasPrevious bool
}

// NewSampler returns a CPU sampler with no previous sample.
func NewSampler() *Sampler {
	return &Sampler{}
}

// Read collects all available sensors and updates the sampler's CPU baseline.
// Missing sensor classes and missing individual attributes are ignored. Errors
// are returned for inaccessible sensor directories or unreadable proc files;
// any readings collected before an error are still returned.
func (s *Sampler) Read(sysRoot, procRoot string) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, errors.New("sensors: nil Sampler")
	}
	if sysRoot == "" {
		sysRoot = defaultSysRoot
	}
	if procRoot == "" {
		procRoot = defaultProcRoot
	}

	snapshot := Snapshot{
		CPU: CPUUsage{
			Label: "CPU",
			Unit:  UnitPercent,
		},
		Memory: MemoryUsage{
			Label:       "Memory",
			Unit:        UnitBytes,
			PercentUnit: UnitPercent,
		},
	}

	var errs []error
	cpuTemps, deviceTemps, err := readTemperatures(sysRoot)
	if err != nil {
		errs = append(errs, err)
	}
	snapshot.CPUTemperatures = cpuTemps
	snapshot.DeviceTemperatures = deviceTemps
	if len(cpuTemps) > 0 {
		hottest := cpuTemps[0]
		for _, temp := range cpuTemps[1:] {
			if temp.Celsius > hottest.Celsius {
				hottest = temp
			}
		}
		snapshot.CPUTemperature = &hottest
	}

	snapshot.Fans, err = readFans(sysRoot)
	if err != nil {
		errs = append(errs, err)
	}
	snapshot.Batteries, snapshot.PowerSupplies, err = readPowerSupplies(sysRoot)
	if err != nil {
		errs = append(errs, err)
	}

	snapshot.CPU, err = s.Sample(procRoot)
	if err != nil {
		errs = append(errs, err)
	}
	snapshot.Memory, err = readMemory(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		errs = append(errs, err)
	}

	return snapshot, errors.Join(errs...)
}

// Sample updates the CPU baseline from procRoot/stat and returns utilization
// since the preceding sample for the same proc root. The first valid sample
// only establishes a baseline.
func (s *Sampler) Sample(procRoot string) (CPUUsage, error) {
	usage := CPUUsage{Label: "CPU", Unit: UnitPercent}
	if s == nil {
		return usage, errors.New("sensors: nil Sampler")
	}
	if procRoot == "" {
		procRoot = defaultProcRoot
	}
	procRoot = filepath.Clean(procRoot)

	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok, err := readCPUTimes(filepath.Join(procRoot, "stat"))
	if err != nil {
		s.previous = cpuTimes{}
		s.hasPrevious = false
		s.procRoot = ""
		return usage, err
	}
	if !ok || s.procRoot != procRoot || !s.hasPrevious {
		s.procRoot = procRoot
		s.previous = current
		s.hasPrevious = ok
		return usage, nil
	}

	utilization, valid := current.utilizationSince(s.previous)
	s.previous = current
	if !valid {
		return usage, nil
	}
	usage.Utilization = utilization
	usage.Available = true
	return usage, nil
}

func wrapPathError(path string, err error) error {
	return fmt.Errorf("sensors: read %s: %w", path, err)
}
