package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"win95linux/internal/sensors"
	"win95linux/internal/ui"
)

func TestGeekMonitorSamplerHistoryAndMissingSensors(t *testing.T) {
	sysRoot := t.TempDir()
	procRoot := t.TempDir()
	writeGeekFile(t, procRoot, "stat", "cpu  100 0 50 800 0 0 0 0 0 0\n")
	writeGeekFile(t, procRoot, "meminfo", "MemTotal: 16384000 kB\nMemAvailable: 4096000 kB\n")

	d := &Desktop{}
	a := &GeekMonitorApp{d: d, sampler: sensors.NewSampler(), sysRoot: sysRoot, procRoot: procRoot}
	for i := range a.cpuHist {
		a.cpuHist[i] = -1
		a.memHist[i] = -1
	}
	a.Tick(d, nil, time.Now())
	if a.snapshot.CPU.Available {
		t.Fatal("first CPU sample should establish a baseline")
	}
	if !a.snapshot.Memory.Available || a.snapshot.Memory.UsedPercent != 75 {
		t.Fatalf("memory = %+v", a.snapshot.Memory)
	}
	if a.cpuHist[geekHistorySize-1] != -1 || a.memHist[geekHistorySize-1] != 75 {
		t.Fatalf("initial history = %v / %v", a.cpuHist[59], a.memHist[59])
	}

	writeGeekFile(t, procRoot, "stat", "cpu  120 10 70 850 10 5 5 0 0 0\n")
	a.Tick(d, nil, time.Now())
	if !a.snapshot.CPU.Available || a.snapshot.CPU.Utilization != 50 {
		t.Fatalf("second CPU sample = %+v", a.snapshot.CPU)
	}
	if a.cpuHist[geekHistorySize-1] != 50 {
		t.Fatalf("CPU history = %v", a.cpuHist[59])
	}

	rows := a.geekSensorRows()
	for _, label := range []string{"CPU 温度", "设备温度", "风扇", "电池", "电源适配器"} {
		found := false
		for _, row := range rows {
			if row.label == label && strings.Contains(row.value, "N/A") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing sensor class %q did not render N/A: %+v", label, rows)
		}
	}
}

func TestGeekMonitorFormatsAllSensorClasses(t *testing.T) {
	a := &GeekMonitorApp{}
	a.snapshot = sensors.Snapshot{
		CPUTemperatures:    []sensors.Temperature{{Label: "Package", Celsius: 47, Unit: sensors.UnitCelsius}},
		DeviceTemperatures: []sensors.Temperature{{Label: "GPU", Celsius: 62.5, Unit: sensors.UnitCelsius}},
		Fans: []sensors.Fan{
			{Label: "CPU Fan", RPM: 1800, Unit: sensors.UnitRPM},
			{Label: "Stopped Fan", RPM: 0, Unit: sensors.UnitRPM},
		},
		Batteries: []sensors.Battery{{
			Name: "BAT0", Label: "Main Battery", Manufacturer: "Example", Status: "Discharging",
			Percent: 73, PercentUnit: sensors.UnitPercent, HasPercent: true,
			EnergyNow: 42, EnergyFull: 60, EnergyFullDesign: 65, EnergyUnit: sensors.UnitWh,
			HasEnergyNow: true, HasEnergyFull: true, HasEnergyFullDesign: true,
			PowerNow: 8.5, PowerUnit: sensors.UnitW, HasPowerNow: true,
		}},
		PowerSupplies: []sensors.PowerSupply{
			{Name: "AC", Label: "AC Adapter", Kind: "Mains", Online: false, HasOnline: true},
			{Name: "ADP1", Label: "ADP1", Kind: "ADP"},
		},
	}
	rows := a.geekSensorRows()
	checks := map[string][]string{
		"Package":     {"47.0 °C"},
		"GPU":         {"62.5 °C"},
		"CPU Fan":     {"1800 RPM"},
		"Stopped Fan": {"0 RPM"},
		"BAT0":        {"73 %", "Discharging", "42.0/60.0 Wh", "设计 65.0 Wh", "功率 8.5 W"},
		"AC":          {"Mains", "未连接"},
		"ADP1":        {"ADP", "N/A"},
	}
	for label, values := range checks {
		var row geekSensorRow
		found := false
		for _, candidate := range rows {
			if candidate.label == label || strings.HasPrefix(candidate.label, label+" ·") || strings.HasPrefix(candidate.label, label+" [") {
				row = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("sensor row %q not found", label)
		}
		for _, value := range values {
			if !strings.Contains(row.value, value) {
				t.Fatalf("row %q = %q, want %q", label, row.value, value)
			}
		}
	}
}

func TestGeekMonitorPaintAndOpenEntry(t *testing.T) {
	d := testDesktop(t)
	d.openGeekMonitor()
	var app *GeekMonitorApp
	var window *Window
	for _, w := range d.windows {
		if candidate, ok := w.App.(*GeekMonitorApp); ok {
			app, window = candidate, w
		}
	}
	if app == nil {
		t.Fatal("openGeekMonitor did not open a window")
	}
	d.openGeekMonitor()
	count := 0
	for _, w := range d.windows {
		if _, ok := w.App.(*GeekMonitorApp); ok {
			count++
		}
	}
	if count != 1 || d.active != window {
		t.Fatal("openGeekMonitor should focus the existing window")
	}
	if len(window.menus) != 0 {
		t.Fatal("integration must not add menus")
	}

	app.snapshot.CPU = sensors.CPUUsage{Utilization: 50, Available: true}
	app.snapshot.Memory = sensors.MemoryUsage{UsedPercent: 75, TotalBytes: 8 << 30, UsedBytes: 6 << 30, Available: true}
	app.sampled = time.Now()
	canvas := ui.NewCanvas(780, 500)
	app.Paint(d, window, canvas, ui.Rect{0, 0, 780, 500})
	if app.listR.H <= 0 || app.scroll.Total == 0 {
		t.Fatal("paint did not lay out the sensor list")
	}
	green := false
	for _, pixel := range canvas.Pix {
		if pixel == 0x008000 {
			green = true
			break
		}
	}
	if !green {
		t.Fatal("CPU bar was not drawn")
	}

	app.snapshot = sensors.Snapshot{}
	small := ui.NewCanvas(620, 380)
	app.Paint(d, window, small, ui.Rect{0, 0, 620, 380})
}

func writeGeekFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
