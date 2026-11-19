package desktop

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/ui"
)

type taskProc struct {
	pid   int
	name  string
	state byte
	rssKB uint64
	ticks uint64
}

type TaskManagerApp struct {
	Base
	d   *Desktop
	win *Window

	procs  []taskProc
	sel    int
	selID  int
	scroll VScroll

	cpuPct    int
	memPct    int
	memUsedKB uint64
	memTotal  uint64
	lastTotal uint64
	lastIdle  uint64
	lastErr   string

	sortCol  int
	sortDesc bool

	lastClick time.Time
	lastSel   int

	btnEnd, btnRefresh      ui.Rect
	listR, headerR, statusR ui.Rect
	press                   string
}

func NewTaskManagerApp(d *Desktop) *TaskManagerApp {
	a := &TaskManagerApp{d: d, sel: -1, selID: -1, lastTotal: 0, lastIdle: 0}
	a.sample()
	return a
}

func (a *TaskManagerApp) Title() string           { return "任务管理器" }
func (a *TaskManagerApp) Icon() ui.IconKind       { return ui.IcoComputer }
func (a *TaskManagerApp) DefaultSize() (int, int) { return 720, 540 }
func (a *TaskManagerApp) MinSize() (int, int)     { return 480, 320 }
func (a *TaskManagerApp) Resizable() bool         { return true }

func (a *TaskManagerApp) Menus(d *Desktop, w *Window) []Menu {
	end := func() { a.endSelected(d) }
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "刷新(R)", Accel: "F5", Enabled: true, Action: func() { a.sample(); d.Invalidate() }},
			{Label: "结束任务(E)", Accel: "Del", Enabled: a.sel >= 0, Action: end},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "按 PID 排序", Checked: a.sortCol == 0, Enabled: true, Action: func() { a.setSort(0) }},
			{Label: "按进程名排序", Checked: a.sortCol == 1, Enabled: true, Action: func() { a.setSort(1) }},
			{Label: "按内存排序", Checked: a.sortCol == 2, Enabled: true, Action: func() { a.setSort(2) }},
			{Separator: true},
			{Label: "降序排列", Checked: a.sortDesc, Enabled: true, Action: func() {
				a.sortDesc = !a.sortDesc
				a.sampleKeep()
				d.Invalidate()
			}},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于任务管理器(A)", Enabled: true, Action: func() {
				d.MessageBox("关于任务管理器",
					"Windows 95 Linux 任务管理器\n\n"+
						"直接读取 /proc/[pid]/stat 显示真实的 PID、进程名与常驻内存；\n"+
						"顶部进度条读取 /proc/stat 与 /proc/meminfo。\n\n"+
						"结束任务会向选中进程发送 SIGKILL。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *TaskManagerApp) setSort(col int) {
	if a.sortCol == col {
		a.sortDesc = !a.sortDesc
	} else {
		a.sortCol = col
		a.sortDesc = col == 2
	}
	a.sampleKeep()
	a.d.Invalidate()
}

func (a *TaskManagerApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	a.sample()
	d.Invalidate()
}

// sample refreshes gauges and the process list. It keeps the selected PID
// across re-sorts so a refresh does not jump to another process.
func (a *TaskManagerApp) sample() {
	a.sampleKeep()
}

func (a *TaskManagerApp) sampleKeep() {
	selID := a.selID
	if selID < 0 && a.sel >= 0 && a.sel < len(a.procs) {
		selID = a.procs[a.sel].pid
	}

	if total, idle, ok := readCPUStat(); ok {
		if a.lastTotal > 0 && total > a.lastTotal {
			dt := total - a.lastTotal
			di := idle
			if idle >= a.lastIdle {
				di = idle - a.lastIdle
			} else {
				di = 0
			}
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
			a.cpuPct = pct
		}
		a.lastTotal, a.lastIdle = total, idle
	}
	if totalKB, availKB, ok := readMemInfo(); ok {
		a.memTotal = totalKB
		if availKB > totalKB {
			availKB = totalKB
		}
		a.memUsedKB = totalKB - availKB
		a.memPct = int(a.memUsedKB * 100 / totalKB)
	}

	procs, err := listProcesses()
	if err != nil {
		a.lastErr = err.Error()
	} else {
		a.lastErr = ""
		a.procs = procs
	}
	a.sortProcs()
	a.sel = -1
	for i := range a.procs {
		if a.procs[i].pid == selID {
			a.sel = i
			break
		}
	}
	a.selID = selID
	if a.sel < 0 && len(a.procs) > 0 {
		a.sel = 0
		a.selID = a.procs[0].pid
	}
	a.scroll.Clamp()
}

func (a *TaskManagerApp) sortProcs() {
	less := func(i, j int) bool {
		x, y := a.procs[i], a.procs[j]
		switch a.sortCol {
		case 1:
			return strings.ToLower(x.name) < strings.ToLower(y.name)
		case 2:
			return x.rssKB < y.rssKB
		default:
			return x.pid < y.pid
		}
	}
	// A stable insertion sort keeps equal keys in PID order and is plenty for
	// the few hundred entries /proc typically exposes.
	for i := 1; i < len(a.procs); i++ {
		for j := i; j > 0; j-- {
			if a.sortDesc {
				if !less(j-1, j) {
					break
				}
			} else if less(j-1, j) {
				break
			}
			a.procs[j-1], a.procs[j] = a.procs[j], a.procs[j-1]
		}
	}
}

func listProcesses() ([]taskProc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	pageSize := uint64(os.Getpagesize())
	out := make([]taskProc, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		p, ok := parseProcStat(string(b))
		if !ok {
			continue
		}
		p.pid = pid
		p.rssKB = p.rssKB * pageSize / 1024
		out = append(out, p)
	}
	return out, nil
}

// parseProcStat extracts the fields we display from /proc/[pid]/stat. The
// process name is wrapped in parentheses and may itself contain spaces or
// parentheses, so it must be located from the outside in.
func parseProcStat(data string) (taskProc, bool) {
	var p taskProc
	lb := strings.IndexByte(data, '(')
	rb := strings.LastIndexByte(data, ')')
	if lb < 0 || rb <= lb {
		return p, false
	}
	p.name = data[lb+1 : rb]
	fields := strings.Fields(data[rb+1:])
	if len(fields) < 22 {
		return p, false
	}
	p.state = fields[0][0]
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	rss, err3 := strconv.ParseUint(fields[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return p, false
	}
	p.ticks = utime + stime
	p.rssKB = rss
	return p, true
}

func (a *TaskManagerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	gaugeW := r.W - 150
	if gaugeW < 120 {
		gaugeW = r.W - 16
	}
	cpuRow := ui.Rect{r.X + 12, r.Y + 8, gaugeW, 26}
	f.Text(c, cpuRow.X, cpuRow.Y, "CPU 使用率", ui.ColText)
	f.TextRight(c, cpuRow.Right(), cpuRow.Y, fmt.Sprintf("%d%%", a.cpuPct), ui.ColText)
	drawGauge(c, ui.Rect{cpuRow.X, cpuRow.Y + 18, cpuRow.W, 9}, a.cpuPct, gaugeColor(a.cpuPct))

	memRow := ui.Rect{r.X + 12, r.Y + 44, gaugeW, 26}
	f.Text(c, memRow.X, memRow.Y, "内存使用率", ui.ColText)
	f.TextRight(c, memRow.Right(), memRow.Y, fmt.Sprintf("%d%%", a.memPct), ui.ColText)
	drawGauge(c, ui.Rect{memRow.X, memRow.Y + 18, memRow.W, 9}, a.memPct, gaugeColor(a.memPct))
	if a.memTotal > 0 {
		f.Text(c, r.X+gaugeW+24, r.Y+8, fmt.Sprintf("已用 %d MB", a.memUsedKB/1024), ui.ColText)
		f.Text(c, r.X+gaugeW+24, r.Y+26, fmt.Sprintf("共 %d MB", a.memTotal/1024), ui.ColShadow)
	}

	toolbarY := r.Y + 78
	a.btnEnd = ui.Rect{r.X + 12, toolbarY, 104, 26}
	a.btnRefresh = ui.Rect{a.btnEnd.Right() + 8, toolbarY, 72, 26}
	ui.Button(c, a.btnEnd, "结束任务", a.press == "end", a.sel >= 0)
	ui.Button(c, a.btnRefresh, "刷新", a.press == "refresh", true)
	info := fmt.Sprintf("%d 个进程", len(a.procs))
	if a.lastErr != "" {
		info = "读取 /proc 失败"
	}
	f.TextRight(c, r.Right()-12, toolbarY+5, info, ui.ColShadow)

	a.headerR = ui.Rect{r.X + 12, toolbarY + 34, r.W - 24, 22}
	c.FillRect(a.headerR, ui.ColFace)
	c.HLine(a.headerR.X, a.headerR.Bottom()-1, a.headerR.W, ui.ColShadow)
	c.HLine(a.headerR.X, a.headerR.Bottom(), a.headerR.W, ui.ColWhite)

	listBottom := r.Bottom() - 26
	a.listR = ui.Rect{a.headerR.X, a.headerR.Bottom() + 1, a.headerR.W, listBottom - a.headerR.Bottom() - 1}
	if a.listR.H < 20 {
		a.listR.H = 20
	}
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)

	inner := a.listR.Inset(2)
	scrollW := 16
	pidW, memW, stateW := 70, 104, 54
	nameW := inner.W - scrollW - pidW - memW - stateW - 4
	if nameW < 90 {
		nameW = 90
	}
	cols := []int{pidW, nameW, memW, stateW}
	heads := []string{"PID", "进程名", "内存", "状态"}
	hx := a.headerR.X + 4
	for i, h := range heads {
		f.Text(c, hx, a.headerR.Y+3, h, ui.ColText)
		hx += cols[i]
	}
	rows := inner.H / 21
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.procs)
	a.scroll.Visible = rows
	a.scroll.Clamp()

	if len(a.procs) == 0 {
		msg := "没有可显示的进程。"
		if a.lastErr != "" {
			msg = "无法读取 /proc：" + a.lastErr
		}
		f.Text(c, inner.X+8, inner.Y+8, msg, ui.ColShadow)
	}
	y := inner.Y
	for i := a.scroll.Pos; i < len(a.procs) && y+20 <= inner.Bottom(); i++ {
		p := a.procs[i]
		rr := ui.Rect{inner.X, y, inner.W - scrollW, 20}
		if i == a.sel {
			c.FillRect(rr, ui.ColNavy)
		}
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		x := rr.X + 4
		f.Text(c, x, rr.Y+2, strconv.Itoa(p.pid), col)
		x += cols[0]
		f.TextClip(c, x, rr.Y+2, cols[1]-6, p.name, col)
		x += cols[1]
		f.TextRight(c, x+cols[2]-8, rr.Y+2, memText(p.rssKB), col)
		x += cols[2]
		f.Text(c, x, rr.Y+2, procStateText(p.state), col)
		y += 21
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H})

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	status := "选择一个进程，然后点击“结束任务”。"
	if a.sel >= 0 && a.sel < len(a.procs) {
		p := a.procs[a.sel]
		status = fmt.Sprintf("PID %d  %s  ·  %s", p.pid, p.name, memText(p.rssKB))
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, status, ui.ColText)
}

func memText(kb uint64) string {
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.1f GB", float64(kb)/(1024*1024))
	}
	if kb >= 1024 {
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	}
	return fmt.Sprintf("%d KB", kb)
}

func procStateText(s byte) string {
	switch s {
	case 'R':
		return "运行"
	case 'S':
		return "睡眠"
	case 'D':
		return "等待"
	case 'T':
		return "暂停"
	case 'Z':
		return "僵尸"
	case 'I':
		return "空闲"
	default:
		return string(s)
	}
}

func (a *TaskManagerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if ev.Kind == MouseWheel {
		if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseMove {
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnEnd.Contains(ev.X, ev.Y):
			a.press = "end"
		case a.btnRefresh.Contains(ev.X, ev.Y):
			a.press = "refresh"
		case a.headerR.Contains(ev.X, ev.Y):
			pidW, memW, stateW := 70, 104, 54
			nameW := a.headerR.W - 16 - pidW - memW - stateW - 4
			if nameW < 90 {
				nameW = 90
			}
			x := ev.X - a.headerR.X - 4
			switch {
			case x >= 0 && x < pidW:
				a.setSort(0)
			case x >= pidW && x < pidW+nameW:
				a.setSort(1)
			case x >= pidW+nameW && x < pidW+nameW+memW:
				a.setSort(2)
			}
		case a.listR.Contains(ev.X, ev.Y):
			if idx := a.rowAt(ev.X, ev.Y); idx >= 0 {
				a.sel = idx
				a.selID = a.procs[idx].pid
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastSel == idx {
					a.endSelected(d)
				}
				a.lastClick = time.Now()
				a.lastSel = idx
				d.Invalidate()
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "end":
			if a.btnEnd.Contains(ev.X, ev.Y) {
				a.endSelected(d)
			}
		case "refresh":
			if a.btnRefresh.Contains(ev.X, ev.Y) {
				a.sampleKeep()
				d.Invalidate()
			}
		}
	}
}

func (a *TaskManagerApp) rowAt(x, y int) int {
	inner := a.listR.Inset(2)
	if y < inner.Y {
		return -1
	}
	row := (y - inner.Y) / 21
	idx := a.scroll.Pos + row
	if idx >= 0 && idx < len(a.procs) {
		return idx
	}
	return -1
}

func (a *TaskManagerApp) endSelected(d *Desktop) {
	if a.sel < 0 || a.sel >= len(a.procs) {
		return
	}
	p := a.procs[a.sel]
	if p.pid <= 1 {
		d.MessageBox("结束任务", fmt.Sprintf("PID %d 是系统的第一个进程，不能被结束。", p.pid), IconWarning, []string{"确定"}, nil)
		return
	}
	if p.pid == os.Getpid() {
		d.MessageBox("结束任务", "不能结束 Windows 95 桌面自身。", IconWarning, []string{"确定"}, nil)
		return
	}
	d.MessageBox("结束任务",
		fmt.Sprintf("确实要结束进程“%s”（PID %d）吗？\n\n该进程会被立即终止，未保存的数据可能丢失。", p.name, p.pid),
		IconWarning, []string{"结束任务", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if err := syscall.Kill(p.pid, syscall.SIGKILL); err != nil {
				d.MessageBox("任务管理器", "无法结束进程：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.sampleKeep()
			d.Invalidate()
		})
}

func (a *TaskManagerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F5":
		a.sampleKeep()
		d.Invalidate()
		return true
	case "Delete":
		a.endSelected(d)
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
			a.selID = a.procs[a.sel].pid
			a.ensureSel()
			d.Invalidate()
		}
		return true
	case "Down":
		if a.sel < len(a.procs)-1 {
			a.sel++
			a.selID = a.procs[a.sel].pid
			a.ensureSel()
			d.Invalidate()
		}
		return true
	}
	return false
}

func (a *TaskManagerApp) ensureSel() {
	if a.sel < 0 {
		return
	}
	rows := a.scroll.Visible
	if rows < 1 {
		rows = 1
	}
	if a.sel < a.scroll.Pos {
		a.scroll.Pos = a.sel
	}
	if a.sel >= a.scroll.Pos+rows {
		a.scroll.Pos = a.sel - rows + 1
	}
}

func (a *TaskManagerApp) Close() {}
