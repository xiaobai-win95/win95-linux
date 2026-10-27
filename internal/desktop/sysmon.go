package desktop

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"win95linux/internal/ui"
)

// sampleSystem refreshes the taskbar CPU and memory gauges. The counters are
// cumulative, so the CPU percentage is calculated from the delta between
// samples; the first call only establishes a baseline.
func (d *Desktop) sampleSystem() {
	if total, idle, ok := readCPUStat(); ok {
		if d.cpuTotal > 0 && total > d.cpuTotal {
			dt := total - d.cpuTotal
			di := idle - d.cpuIdle
			if di > dt {
				di = dt
			}
			pct := int((dt - di) * 100 / dt)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			d.cpuPct = pct
		}
		d.cpuTotal, d.cpuIdle = total, idle
	}
	if pct, ok := readMemPercent(); ok {
		d.memPct = pct
	}
}

func readCPUStat() (total, idle uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		total += v
		// idle and iowait are the non-busy counters.
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return total, idle, total > 0
}

func readMemInfo() (totalKB, availKB uint64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	return totalKB, availKB, totalKB > 0
}

func readMemPercent() (int, bool) {
	total, avail, ok := readMemInfo()
	if !ok {
		return 0, false
	}
	if avail > total {
		avail = total
	}
	return int((total - avail) * 100 / total), true
}

func gaugeColor(pct int) uint32 {
	switch {
	case pct >= 85:
		return ui.ColRed
	case pct >= 60:
		return 0xB08000
	default:
		return 0x008000
	}
}

func (d *Desktop) clockRect() ui.Rect {
	top := d.taskbarTop()
	return ui.Rect{d.W - clockW - 4, top + 5, clockW, 24}
}

func (d *Desktop) sysTrayRect() ui.Rect {
	c := d.clockRect()
	return ui.Rect{c.X - sysTrayW - 4, c.Y, sysTrayW, c.H}
}

func drawGauge(c *ui.Canvas, r ui.Rect, pct int, col uint32) {
	c.FillRect(r, ui.ColWhite)
	c.Frame(r, ui.ColShadow)
	innerW := r.W - 2
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	fill := innerW * pct / 100
	if fill > 0 {
		c.FillRect(ui.Rect{r.X + 1, r.Y + 1, fill, r.H - 2}, col)
	}
}

func (d *Desktop) drawSysTray(c *ui.Canvas) {
	r := d.sysTrayRect()
	c.FillRect(r, ui.ColFace)
	c.Sunken(r)

	rows := []struct {
		label string
		pct   int
	}{
		{"CPU", d.cpuPct},
		{"RAM", d.memPct},
	}
	for i, row := range rows {
		y := r.Y + 4 + i*9
		ui.TinyText(c, r.X+4, y, fmt.Sprintf("%s%3d%%", row.label, row.pct), ui.ColText)
		drawGauge(c, ui.Rect{r.X + 36, y, r.W - 40, 5}, row.pct, gaugeColor(row.pct))
	}
}

func (d *Desktop) showSysMonitor() {
	totalKB, availKB, _ := readMemInfo()
	load := "不可用"
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			load = f[0] + "  " + f[1] + "  " + f[2]
		}
	}
	uptime := "不可用"
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if sec, err := strconv.ParseFloat(f[0], 64); err == nil {
				uptime = time.Duration(sec * float64(time.Second)).Round(time.Second).String()
			}
		}
	}
	usedMB := (totalKB - availKB) / 1024
	totalMB := totalKB / 1024
	text := fmt.Sprintf("CPU 占用：%d%%\n内存占用：%d%%（%d MB / %d MB）\n系统负载：%s\n运行时间：%s",
		d.cpuPct, d.memPct, usedMB, totalMB, load, uptime)
	d.MessageBox("系统监视器", text, IconInfo, []string{"确定"}, nil)
}
