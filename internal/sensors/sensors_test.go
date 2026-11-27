package sensors_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"win95linux/internal/sensors"
)

func TestReadSyntheticFilesystem(t *testing.T) {
	sysRoot := t.TempDir()
	procRoot := t.TempDir()

	writeFile(t, sysRoot, "class/thermal/thermal_zone0/type", "cpu-thermal\n")
	writeFile(t, sysRoot, "class/thermal/thermal_zone0/temp", "45125\n")
	writeFile(t, sysRoot, "class/thermal/thermal_zone1/type", "GPU\n")
	writeFile(t, sysRoot, "class/thermal/thermal_zone1/temp", "62750\n")
	writeFile(t, sysRoot, "class/thermal/thermal_zone2/type", "unsupported\n")

	writeFile(t, sysRoot, "class/hwmon/hwmon0/name", "coretemp\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp1_input", "47000\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp1_label", "Package\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp2_input", "51000\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp2_label", "Core 0\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp3_input", "invalid\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/fan1_input", "1800\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/fan1_label", "CPU Fan\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/fan2_input", "0\n")

	writeFile(t, sysRoot, "class/hwmon/hwmon1/name", "nvme\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon1/temp1_input", "40000\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon1/temp1_label", "Composite\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon1/temp2_input", "-273200\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon1/fan1_input", "-5\n")

	writeFile(t, sysRoot, "class/power_supply/BAT0/manufacturer_name", "Example\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/model_name", "Main Battery\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/capacity", "73\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/status", "Discharging\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/energy_now", "42000000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/energy_full", "60000000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/energy_full_design", "65000000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/power_now", "8500000\n")

	// BAT1 has no energy or power attributes; charge, voltage, and current
	// provide the fallback values.
	writeFile(t, sysRoot, "class/power_supply/BAT1/charge_now", "2000000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT1/charge_full", "4000000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT1/voltage_now", "3700000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT1/current_now", "-500000\n")
	writeFile(t, sysRoot, "class/power_supply/BAT1/status", "Not charging\n")

	writeFile(t, sysRoot, "class/power_supply/AC/type", "Mains\n")
	writeFile(t, sysRoot, "class/power_supply/AC/model_name", "AC Adapter\n")
	writeFile(t, sysRoot, "class/power_supply/AC/online", "1\n")
	writeFile(t, sysRoot, "class/power_supply/ADP1/online", "0\n")

	writeFile(t, procRoot, "stat", "cpu  100 0 50 800 0 0 0 0 0 0\ncpu0 100 0 50 800 0 0 0 0 0 0\n")
	writeFile(t, procRoot, "meminfo", "MemTotal:       16384000 kB\nMemAvailable:    4096000 kB\nMemFree:          102400 kB\n")

	sampler := sensors.NewSampler()
	snapshot, err := sampler.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CPU.Available {
		t.Fatal("first CPU sample unexpectedly has utilization")
	}
	if len(snapshot.CPUTemperatures) != 3 || len(snapshot.DeviceTemperatures) != 2 {
		t.Fatalf("temperatures = %d CPU, %d device; want 3, 2", len(snapshot.CPUTemperatures), len(snapshot.DeviceTemperatures))
	}
	if snapshot.CPUTemperature == nil || !close(snapshot.CPUTemperature.Celsius, 51) {
		t.Fatalf("CPUTemperature = %+v, want hottest 51 C", snapshot.CPUTemperature)
	}
	assertTemperature(t, snapshot.CPUTemperatures, "class/thermal/thermal_zone0/temp", "cpu-thermal", 45.125)
	assertTemperature(t, snapshot.CPUTemperatures, "class/hwmon/hwmon0/temp1_input", "Package", 47)
	assertTemperature(t, snapshot.DeviceTemperatures, "class/thermal/thermal_zone1/temp", "GPU", 62.75)
	assertTemperature(t, snapshot.DeviceTemperatures, "class/hwmon/hwmon1/temp1_input", "Composite", 40)

	if len(snapshot.Fans) != 2 {
		t.Fatalf("fans = %+v, want two valid inputs", snapshot.Fans)
	}
	if snapshot.Fans[0].Label != "CPU Fan" || snapshot.Fans[0].RPM != 1800 || snapshot.Fans[0].Unit != sensors.UnitRPM {
		t.Fatalf("fan1 = %+v", snapshot.Fans[0])
	}
	if snapshot.Fans[1].Label != "coretemp fan2" || snapshot.Fans[1].RPM != 0 {
		t.Fatalf("stopped fan = %+v", snapshot.Fans[1])
	}

	if len(snapshot.Batteries) != 2 {
		t.Fatalf("batteries = %+v, want two", snapshot.Batteries)
	}
	battery := snapshot.Batteries[0]
	if battery.Label != "Main Battery" || battery.Status != "Discharging" || !battery.HasPercent || battery.Percent != 73 {
		t.Fatalf("BAT0 identity/status = %+v", battery)
	}
	if !battery.HasEnergyNow || !close(battery.EnergyNow, 42) || !battery.HasEnergyFull || !close(battery.EnergyFull, 60) || !close(battery.EnergyFullDesign, 65) {
		t.Fatalf("BAT0 energy = %+v", battery)
	}
	if !battery.HasPowerNow || !close(battery.PowerNow, 8.5) || battery.EnergyUnit != sensors.UnitWh || battery.PowerUnit != sensors.UnitW {
		t.Fatalf("BAT0 power/units = %+v", battery)
	}
	battery = snapshot.Batteries[1]
	if !battery.HasPercent || !close(battery.Percent, 50) || !battery.HasEnergyNow || !close(battery.EnergyNow, 7.4) || !battery.HasEnergyFull || !close(battery.EnergyFull, 14.8) {
		t.Fatalf("BAT1 fallback energy/percent = %+v", battery)
	}
	if !battery.HasPowerNow || !close(battery.PowerNow, -1.85) {
		t.Fatalf("BAT1 fallback power = %+v", battery)
	}

	if len(snapshot.PowerSupplies) != 2 {
		t.Fatalf("power supplies = %+v, want AC and ADP", snapshot.PowerSupplies)
	}
	if supply := snapshot.PowerSupplies[0]; supply.Name != "AC" || supply.Kind != "Mains" || !supply.HasOnline || !supply.Online {
		t.Fatalf("AC supply = %+v", supply)
	}
	if supply := snapshot.PowerSupplies[1]; supply.Name != "ADP1" || supply.Kind != "ADP" || !supply.HasOnline || supply.Online {
		t.Fatalf("ADP supply = %+v", supply)
	}

	if !snapshot.Memory.Available || snapshot.Memory.TotalBytes != 16384000*1024 || snapshot.Memory.AvailableBytes != 4096000*1024 || snapshot.Memory.UsedBytes != 12288000*1024 || !close(snapshot.Memory.UsedPercent, 75) {
		t.Fatalf("memory = %+v", snapshot.Memory)
	}

	writeFile(t, procRoot, "stat", "cpu  120 10 70 850 10 5 5 0 0 0\ncpu0 120 10 70 850 10 5 5 0 0 0\n")
	snapshot, err = sampler.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CPU.Available || !close(snapshot.CPU.Utilization, 50) || snapshot.CPU.Unit != sensors.UnitPercent {
		t.Fatalf("CPU delta = %+v, want 50%%", snapshot.CPU)
	}

	// The package-level convenience reader remains stateless.
	snapshot, err = sensors.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CPU.Available || !snapshot.Memory.Available {
		t.Fatalf("convenience Read CPU/memory = %+v / %+v", snapshot.CPU, snapshot.Memory)
	}
}

func TestSamplerCPUAccounting(t *testing.T) {
	procRoot := t.TempDir()
	sampler := sensors.NewSampler()

	writeFile(t, procRoot, "stat", "cpu 10 10 10 70 10 0 0 5 20 5\n")
	usage, err := sampler.Sample(procRoot)
	if err != nil || usage.Available {
		t.Fatalf("baseline = %+v, %v", usage, err)
	}

	// Guest time changed but must not be double-counted. The total delta is 50
	// ticks, of which 20 are idle or iowait, yielding 60% utilization.
	writeFile(t, procRoot, "stat", "cpu 20 10 20 90 10 10 0 5 40 10\n")
	usage, err = sampler.Sample(procRoot)
	if err != nil || !usage.Available || !close(usage.Utilization, 60) {
		t.Fatalf("delta = %+v, %v; want 60%%", usage, err)
	}

	writeFile(t, procRoot, "stat", "cpu 20 10 20 90 10 10 0 5 40 10\n")
	usage, err = sampler.Sample(procRoot)
	if err != nil || usage.Available {
		t.Fatalf("zero delta = %+v, %v", usage, err)
	}

	// A counter reset establishes a new baseline instead of producing a huge
	// unsigned delta.
	writeFile(t, procRoot, "stat", "cpu 1 1 1 1 1 1 1 1 0 0\n")
	usage, err = sampler.Sample(procRoot)
	if err != nil || usage.Available {
		t.Fatalf("counter reset = %+v, %v", usage, err)
	}

	otherRoot := t.TempDir()
	writeFile(t, otherRoot, "stat", "cpu 100 100 100 100 100 100 100 100 0 0\n")
	usage, err = sampler.Sample(otherRoot)
	if err != nil || usage.Available {
		t.Fatalf("new proc root = %+v, %v", usage, err)
	}
}

func TestReadMissingAndUnsupportedSensors(t *testing.T) {
	sysRoot := t.TempDir()
	procRoot := t.TempDir()
	snapshot, err := sensors.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CPUTemperatures)+len(snapshot.DeviceTemperatures)+len(snapshot.Fans)+len(snapshot.Batteries)+len(snapshot.PowerSupplies) != 0 {
		t.Fatalf("missing roots produced readings: %+v", snapshot)
	}
	if snapshot.CPU.Available || snapshot.Memory.Available || snapshot.CPUTemperature != nil {
		t.Fatalf("missing roots produced available metrics: %+v", snapshot)
	}
	if snapshot.CPU.Unit != sensors.UnitPercent || snapshot.Memory.Unit != sensors.UnitBytes {
		t.Fatalf("missing metrics lack units: %+v", snapshot)
	}

	writeFile(t, sysRoot, "class/thermal/thermal_zone0/type", "cpu-thermal\n")
	writeFile(t, sysRoot, "class/thermal/thermal_zone0/temp", "not-a-number\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/temp1_input", "NaN\n")
	writeFile(t, sysRoot, "class/hwmon/hwmon0/fan1_input", "-1\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/capacity", "101\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/energy_now", "invalid\n")
	writeFile(t, sysRoot, "class/power_supply/BAT0/power_now", "invalid\n")
	writeFile(t, sysRoot, "class/power_supply/AC/online", "2\n")

	snapshot, err = sensors.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CPUTemperatures)+len(snapshot.DeviceTemperatures)+len(snapshot.Fans) != 0 {
		t.Fatalf("invalid sysfs values produced readings: %+v", snapshot)
	}
	if len(snapshot.Batteries) != 1 || snapshot.Batteries[0].HasPercent || snapshot.Batteries[0].HasEnergyNow || snapshot.Batteries[0].HasPowerNow {
		t.Fatalf("unsupported battery = %+v", snapshot.Batteries)
	}
	if len(snapshot.PowerSupplies) != 1 || snapshot.PowerSupplies[0].HasOnline {
		t.Fatalf("unsupported adapter = %+v", snapshot.PowerSupplies)
	}
}

func TestMemoryFallbackWithoutMemAvailable(t *testing.T) {
	procRoot := t.TempDir()
	writeFile(t, procRoot, "meminfo", "MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 200 kB\nSReclaimable: 25 kB\nShmem: 20 kB\n")
	snapshot, err := sensors.Read(t.TempDir(), procRoot)
	if err != nil {
		t.Fatal(err)
	}
	memory := snapshot.Memory
	if !memory.Available || memory.TotalBytes != 1000*1024 || memory.AvailableBytes != 355*1024 || memory.UsedBytes != 645*1024 || !close(memory.UsedPercent, 64.5) {
		t.Fatalf("fallback memory = %+v", memory)
	}
}

func TestReadFollowsSysfsClassSymlinks(t *testing.T) {
	sysRoot := t.TempDir()
	procRoot := t.TempDir()
	writeFile(t, sysRoot, "devices/thermal_zone0/type", "cpu-thermal\n")
	writeFile(t, sysRoot, "devices/thermal_zone0/temp", "55000\n")
	writeFile(t, sysRoot, "devices/hwmon0/name", "coretemp\n")
	writeFile(t, sysRoot, "devices/hwmon0/fan1_input", "1200\n")

	if err := os.MkdirAll(filepath.Join(sysRoot, "class", "thermal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sysRoot, "class", "hwmon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "devices", "thermal_zone0"), filepath.Join(sysRoot, "class", "thermal", "thermal_zone0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "devices", "hwmon0"), filepath.Join(sysRoot, "class", "hwmon", "hwmon0")); err != nil {
		t.Fatal(err)
	}

	snapshot, err := sensors.Read(sysRoot, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertTemperature(t, snapshot.CPUTemperatures, "class/thermal/thermal_zone0/temp", "cpu-thermal", 55)
	if len(snapshot.Fans) != 1 || snapshot.Fans[0].RPM != 1200 {
		t.Fatalf("symlink fans = %+v", snapshot.Fans)
	}
}

func writeFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertTemperature(t *testing.T, temperatures []sensors.Temperature, source, label string, celsius float64) {
	t.Helper()
	for _, temp := range temperatures {
		if temp.Source == source {
			if temp.Label != label || !close(temp.Celsius, celsius) || temp.Unit != sensors.UnitCelsius {
				t.Fatalf("temperature %s = %+v", source, temp)
			}
			return
		}
	}
	t.Fatalf("temperature %s not found in %+v", source, temperatures)
}

func close(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}
