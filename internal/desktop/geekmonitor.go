package desktop

import (
	"fmt"
	"math"
	"strings"
	"time"

	"win95linux/internal/sensors"
	"win95linux/internal/ui"
)

const geekHistorySize = 60

// GeekMonitorApp presents the sensors package as a compact Windows 95 resource
// monitor. Sensor values are sampled on the desktop's one-second Tick.
type GeekMonitorApp struct {
	Base
	d       *Desktop
	win     *Window
	sampler *sensors.Sampler

	sysRoot  string
	procRoot string
	snapshot sensors.Snapshot
	err      error
	sampled  time.Time

	cpuHist [geekHistorySize]float64
	memHist [geekHistorySize]float64

	scroll  VScroll
	listR   ui.Rect
	statusR ui.Rect
}

func NewGeekMonitorApp(d *Desktop) *GeekMonitorApp {
	a := &GeekMonitorApp{d: d, sampler: sensors.NewSampler()}
	for i := range a.cpuHist {
		a.cpuHist[i] = -1
		a.memHist[i] = -1
	}
	a.sample()
	return a
}

func (d *Desktop) openGeekMonitor() {
	for _, w := range d.windows {
		if a, ok := w.App.(*GeekMonitorApp); ok {
			a.win = w
			d.bringToFront(w)
			return
		}
	}
	a := NewGeekMonitorApp(d)
	w := d.Open(a)
	a.win = w
}

func (a *GeekMonitorApp) Title() string     { return a.d.tr("资源监视器") }
func (a *GeekMonitorApp) Icon() ui.IconKind { return ui.IcoMonitor }
func (a *GeekMonitorApp) DefaultSize() (int, int) {
	return 780, 560
}
func (a *GeekMonitorApp) MinSize() (int, int) { return 620, 440 }
func (a *GeekMonitorApp) Resizable() bool     { return true }

func (a *GeekMonitorApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	a.sample()
	d.Invalidate()
}

func (a *GeekMonitorApp) sample() {
	if a.sampler == nil {
		a.sampler = sensors.NewSampler()
	}
	// Keep partial readings when one sensor class fails: Snapshot deliberately
	// contains all values collected before the error.
	a.snapshot, a.err = a.sampler.Read(a.sysRoot, a.procRoot)
	a.sampled = time.Now()
	pushGeekHistory(&a.cpuHist, a.snapshot.CPU.Utilization, a.snapshot.CPU.Available)
	pushGeekHistory(&a.memHist, a.snapshot.Memory.UsedPercent, a.snapshot.Memory.Available)
}

func pushGeekHistory(history *[geekHistorySize]float64, value float64, available bool) {
	copy(history[:], history[1:])
	if !available || math.IsNaN(value) || math.IsInf(value, 0) {
		history[len(history)-1] = -1
		return
	}
	history[len(history)-1] = value
}

func (a *GeekMonitorApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.TextBold(c, r.X+12, r.Y+8, d.tr("硬件资源 / 传感器"), ui.ColText)
	updated := d.tr("尚未采样")
	if !a.sampled.IsZero() {
		updated = d.trf("更新 %s", a.sampled.Format("15:04:05"))
	}
	f.TextRight(c, r.Right()-12, r.Y+8, updated, ui.ColShadow)

	panelY := r.Y + 32
	panelH := 140
	gap := 10
	panelW := (r.W - 24 - gap) / 2
	if panelW < 100 {
		panelW = 100
	}
	cpuValue, cpuOK := geekPercentText(a.snapshot.CPU.Utilization, a.snapshot.CPU.Available)
	memValue := "N/A"
	memDetail := d.tr("内存传感器不可用")
	memOK := false
	if a.snapshot.Memory.Available {
		memValue, memOK = geekPercentText(a.snapshot.Memory.UsedPercent, true)
		memDetail = fmt.Sprintf("%s / %s", geekBytes(a.snapshot.Memory.UsedBytes), geekBytes(a.snapshot.Memory.TotalBytes))
	}
	a.drawGeekPanel(c, f, ui.Rect{r.X + 12, panelY, panelW, panelH}, d.tr("CPU 使用率"), cpuValue, a.snapshot.CPU.Utilization, d.tr("两次采样后显示利用率"), cpuOK, a.cpuHist, 0x008000)
	a.drawGeekPanel(c, f, ui.Rect{r.X + 12 + panelW + gap, panelY, panelW, panelH}, d.tr("内存使用率"), memValue, a.snapshot.Memory.UsedPercent, memDetail, memOK, a.memHist, ui.ColNavy)

	listY := panelY + panelH + 10
	a.listR = ui.Rect{r.X + 12, listY, r.W - 24, r.Bottom() - listY - 28}
	if a.listR.H < 24 {
		a.listR.H = 24
	}
	c.FillRect(a.listR, ui.ColField)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	rows := a.geekSensorRows()
	scrollW := 16
	a.scroll.Total = len(rows)
	a.scroll.Visible = maxInt(1, inner.H/20)
	a.scroll.Clamp()
	y := inner.Y
	for i := a.scroll.Pos; i < len(rows) && y+20 <= inner.Bottom(); i++ {
		row := rows[i]
		rr := ui.Rect{inner.X, y, inner.W - scrollW, 20}
		if row.section {
			c.FillRect(rr, ui.ColFace)
			f.TextBold(c, rr.X+6, rr.Y+2, row.label, ui.ColText)
		} else {
			labelW := maxInt(80, (rr.W-12)*40/100)
			f.TextClip(c, rr.X+6, rr.Y+2, labelW-8, row.label, ui.ColText)
			f.TextClip(c, rr.X+labelW, rr.Y+2, rr.W-labelW-6, row.value, ui.ColText)
		}
		y += 20
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H})

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	status := d.tr("数据来自 /sys 与 /proc；缺失或首次 CPU 采样显示 N/A。")
	if a.err != nil {
		status = d.tr("部分传感器读取失败：") + strings.ReplaceAll(a.err.Error(), "\n", "; ")
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+3, a.statusR.W-12, status, ui.ColText)
}

func (a *GeekMonitorApp) drawGeekPanel(c *ui.Canvas, f *ui.Font, r ui.Rect, title, value string, percent float64, detail string, available bool, history [geekHistorySize]float64, color uint32) {
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	f.TextBold(c, r.X+10, r.Y+8, title, ui.ColText)
	f.TextRight(c, r.Right()-10, r.Y+8, value, ui.ColText)
	drawGeekMeter(c, f, ui.Rect{r.X + 10, r.Y + 36, r.W - 20, 20}, value, percent, available, color)
	f.TextClip(c, r.X+10, r.Y+64, r.W-20, detail, ui.ColShadow)
	f.Text(c, r.X+10, r.Y+86, a.d.tr("60 秒历史"), ui.ColShadow)
	drawGeekHistory(c, ui.Rect{r.X + 10, r.Y + 104, r.W - 20, r.H - 112}, history, color)
}

func drawGeekMeter(c *ui.Canvas, f *ui.Font, r ui.Rect, value string, percent float64, available bool, color uint32) {
	c.FillRect(r, ui.ColField)
	c.Sunken(r)
	if !available {
		f.Text(c, r.X+(r.W-f.Width(value))/2, r.Y+2, value, ui.ColShadow)
		return
	}
	pct := geekClampPercent(percent)
	inner := r.Inset(3)
	filled := inner.W * pct / 100
	for x := 0; x < filled; x += 7 {
		w := 5
		if x+w > filled {
			w = filled - x
		}
		if w > 0 {
			c.FillRect(ui.Rect{inner.X + x, inner.Y, w, inner.H}, color)
		}
	}
}

func drawGeekHistory(c *ui.Canvas, r ui.Rect, history [geekHistorySize]float64, color uint32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	c.FillRect(r, 0x000000)
	c.Frame(r, ui.ColShadow)
	for i := 1; i < 4; i++ {
		y := r.Y + r.H*i/4
		for x := r.X + 1; x < r.Right()-1; x += 6 {
			c.Set(x, y, 0x004020)
		}
	}
	for i, value := range history {
		if value < 0 {
			continue
		}
		x := r.X + 1 + (r.W-2)*i/len(history)
		nextX := r.X + 1 + (r.W-2)*(i+1)/len(history)
		h := (r.H - 2) * geekClampPercent(value) / 100
		if nextX > x && h > 0 {
			c.FillRect(ui.Rect{x, r.Bottom() - 1 - h, nextX - x - 1, h}, color)
		}
	}
}

type geekSensorRow struct {
	label   string
	value   string
	section bool
}

func (a *GeekMonitorApp) geekSensorRows() []geekSensorRow {
	d := a.d
	if d == nil {
		d = &Desktop{}
	}
	s := a.snapshot
	rows := []geekSensorRow{{label: d.tr("CPU 温度"), section: true}}
	if len(s.CPUTemperatures) == 0 {
		rows = append(rows, geekSensorRow{label: d.tr("CPU 温度"), value: d.tr("N/A（无传感器）")})
	}
	for _, temp := range s.CPUTemperatures {
		rows = append(rows, geekSensorRow{label: geekOneLine(temp.Label), value: fmt.Sprintf("%.1f %s", temp.Celsius, geekUnit(temp.Unit, sensors.UnitCelsius))})
	}
	rows = append(rows, geekSensorRow{label: d.tr("设备温度"), section: true})
	if len(s.DeviceTemperatures) == 0 {
		rows = append(rows, geekSensorRow{label: d.tr("设备温度"), value: d.tr("N/A（无传感器）")})
	}
	for _, temp := range s.DeviceTemperatures {
		rows = append(rows, geekSensorRow{label: geekOneLine(temp.Label), value: fmt.Sprintf("%.1f %s", temp.Celsius, geekUnit(temp.Unit, sensors.UnitCelsius))})
	}
	rows = append(rows, geekSensorRow{label: d.tr("风扇"), section: true})
	if len(s.Fans) == 0 {
		rows = append(rows, geekSensorRow{label: d.tr("风扇"), value: d.tr("N/A（无传感器）")})
	}
	for _, fan := range s.Fans {
		rows = append(rows, geekSensorRow{label: geekOneLine(fan.Label), value: fmt.Sprintf("%.0f %s", fan.RPM, geekUnit(fan.Unit, sensors.UnitRPM))})
	}
	rows = append(rows, geekSensorRow{label: d.tr("电池"), section: true})
	if len(s.Batteries) == 0 {
		rows = append(rows, geekSensorRow{label: d.tr("电池"), value: d.tr("N/A（无传感器）")})
	}
	for _, battery := range s.Batteries {
		reading := geekBatteryValue(battery)
		status := reading.status
		if status == "" {
			status = d.tr("状态 N/A")
		}
		value := d.trf("%s · %s · 能量 %s · 设计 %s · 功率 %s", reading.percent, status, reading.energy, reading.design, reading.power)
		rows = append(rows, geekSensorRow{label: geekBatteryLabel(battery), value: value})
	}
	rows = append(rows, geekSensorRow{label: d.tr("电源"), section: true})
	if len(s.PowerSupplies) == 0 {
		rows = append(rows, geekSensorRow{label: d.tr("电源适配器"), value: d.tr("N/A（无传感器）")})
	}
	for _, supply := range s.PowerSupplies {
		online := "N/A"
		if supply.HasOnline {
			if supply.Online {
				online = d.tr("已连接")
			} else {
				online = d.tr("未连接")
			}
		}
		rows = append(rows, geekSensorRow{
			label: geekOneLine(supply.Name + " · " + supply.Label),
			value: fmt.Sprintf("%s · %s", geekOneLine(supply.Kind), online),
		})
	}
	return rows
}

func geekBatteryLabel(b sensors.Battery) string {
	label := b.Name
	if b.Label != "" && b.Label != b.Name {
		label += " · " + b.Label
	}
	if b.Manufacturer != "" {
		label += " [" + b.Manufacturer + "]"
	}
	return geekOneLine(label)
}

type geekBatteryValueParts struct {
	percent string
	status  string
	energy  string
	design  string
	power   string
}

func geekBatteryValue(b sensors.Battery) geekBatteryValueParts {
	percent := "N/A"
	if b.HasPercent {
		percent = fmt.Sprintf("%.0f %s", b.Percent, geekUnit(b.PercentUnit, sensors.UnitPercent))
	}
	status := geekOneLine(b.Status)
	energy := "N/A"
	if b.HasEnergyNow || b.HasEnergyFull {
		now := "N/A"
		full := "N/A"
		if b.HasEnergyNow {
			now = fmt.Sprintf("%.1f", b.EnergyNow)
		}
		if b.HasEnergyFull {
			full = fmt.Sprintf("%.1f", b.EnergyFull)
		}
		energy = now + "/" + full + " " + geekUnit(b.EnergyUnit, sensors.UnitWh)
	}
	design := "N/A"
	if b.HasEnergyFullDesign {
		design = fmt.Sprintf("%.1f %s", b.EnergyFullDesign, geekUnit(b.EnergyUnit, sensors.UnitWh))
	}
	power := "N/A"
	if b.HasPowerNow {
		power = fmt.Sprintf("%.1f %s", b.PowerNow, geekUnit(b.PowerUnit, sensors.UnitW))
	}
	return geekBatteryValueParts{percent: percent, status: status, energy: energy, design: design, power: power}
}

func geekPercentText(value float64, available bool) (string, bool) {
	if !available || math.IsNaN(value) || math.IsInf(value, 0) {
		return "N/A", false
	}
	return fmt.Sprintf("%.1f%%", value), true
}

func geekClampPercent(value float64) int {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	if value >= 100 {
		return 100
	}
	return int(value)
}

func geekBytes(value uint64) string {
	const unit = 1024
	if value >= unit*unit*unit {
		return fmt.Sprintf("%.1f GB", float64(value)/(unit*unit*unit))
	}
	if value >= unit*unit {
		return fmt.Sprintf("%.1f MB", float64(value)/(unit*unit))
	}
	if value >= unit {
		return fmt.Sprintf("%.1f KB", float64(value)/unit)
	}
	return fmt.Sprintf("%d B", value)
}

func geekUnit(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func geekOneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (a *GeekMonitorApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if ev.Kind == MouseWheel || ev.Kind == MouseMove {
		if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
			d.Invalidate()
		}
	}
}

func (a *GeekMonitorApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Up":
		a.scroll.ScrollBy(-1)
	case "Down":
		a.scroll.ScrollBy(1)
	case "PageUp":
		a.scroll.ScrollBy(-maxInt(1, a.scroll.Visible))
	case "PageDown":
		a.scroll.ScrollBy(maxInt(1, a.scroll.Visible))
	default:
		return false
	}
	d.Invalidate()
	return true
}
