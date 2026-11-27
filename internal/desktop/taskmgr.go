package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/ui"
)

type taskProc struct {
	pid    int
	ppid   int
	name   string
	state  byte
	rssKB  uint64
	ticks  uint64
	cpuPct float64
}

type taskRow struct {
	idx   int
	depth int
	last  bool
}

type TaskManagerApp struct {
	Base
	d   *Desktop
	win *Window

	procs    []taskProc
	order    []taskRow
	kids     map[int]int
	expanded map[int]bool
	sel      int
	selID    int
	scroll   VScroll

	tab  int
	tree bool

	cpuPct    int
	memPct    int
	memUsedKB uint64
	memTotal  uint64
	lastTotal uint64
	lastIdle  uint64
	lastErr   string

	lastProcTicks map[int]uint64

	sortCol  int
	sortDesc bool

	lastClick    time.Time
	lastSel      int
	lastClickPos int

	netRateRx, netRateTx uint64
	diskRateR, diskRateW uint64
	lastNetRx, lastNetTx uint64
	lastDiskR, lastDiskW uint64
	lastCounters         time.Time
	counters             bool

	cpuHist [60]int
	memHist [60]int
	rxHist  [60]uint64
	txHist  [60]uint64
	dRHist  [60]uint64
	dWHist  [60]uint64
	loadAvg string
	uptime  string

	btnEnd, btnRefresh, btnTree ui.Rect
	tabProcBtn, tabPerfBtn      ui.Rect
	listR, headerR, statusR     ui.Rect
	press                       string
}

func NewTaskManagerApp(d *Desktop) *TaskManagerApp {
	a := &TaskManagerApp{
		d:             d,
		sel:           -1,
		selID:         -1,
		tab:           0,
		tree:          true,
		expanded:      map[int]bool{},
		kids:          map[int]int{},
		lastProcTicks: map[int]uint64{},
	}
	a.sample()
	return a
}

func (a *TaskManagerApp) Title() string           { return "系统监视器" }
func (a *TaskManagerApp) Icon() ui.IconKind       { return ui.IcoMonitor }
func (a *TaskManagerApp) DefaultSize() (int, int) { return 800, 580 }
func (a *TaskManagerApp) MinSize() (int, int)     { return 600, 420 }
func (a *TaskManagerApp) Resizable() bool         { return true }

func (a *TaskManagerApp) showTab(tab int) {
	a.tab = tab
	a.d.Invalidate()
}

func (a *TaskManagerApp) Menus(d *Desktop, w *Window) []Menu {
	end := func() { a.endSelected(d) }
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "刷新(R)", Accel: "F5", Enabled: true, Action: func() { a.sampleKeep(); a.d.Invalidate() }},
			{Label: "结束任务(E)", Accel: "Del", Enabled: a.sel >= 0, Action: end},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "进程列表", Checked: a.tab == 0, Enabled: true, Action: func() { a.showTab(0) }},
			{Label: "性能图表", Checked: a.tab == 1, Enabled: true, Action: func() { a.showTab(1) }},
			{Separator: true},
			{Label: "显示进程树", Checked: a.tree, Enabled: true, Action: func() {
				a.tree = !a.tree
				a.buildOrder()
				a.d.Invalidate()
			}},
			{Label: "按 PID 排序", Checked: a.sortCol == 0, Enabled: true, Action: func() { a.setSort(0) }},
			{Label: "按进程名排序", Checked: a.sortCol == 1, Enabled: true, Action: func() { a.setSort(1) }},
			{Label: "按内存排序", Checked: a.sortCol == 2, Enabled: true, Action: func() { a.setSort(2) }},
			{Label: "按 CPU 排序", Checked: a.sortCol == 3, Enabled: true, Action: func() { a.setSort(3) }},
			{Label: "降序排列", Checked: a.sortDesc, Enabled: true, Action: func() {
				a.sortDesc = !a.sortDesc
				a.sampleKeep()
				a.d.Invalidate()
			}},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于系统监视器(A)", Enabled: true, Action: func() {
				d.MessageBox("关于系统监视器",
					"Windows 95 Linux 系统监视器\n\n"+
						"CPU、内存、网络与磁盘数据来自 /proc；\n"+
						"进程树来自 /proc/[pid]/stat 的父进程字段。\n\n"+
						"右键进程可以结束它、限制 CPU（cgroup v2）\n"+
						"或对整机网络应用 tc 限速。",
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
		a.sortDesc = col == 2 || col == 3
	}
	a.sampleKeep()
	a.d.Invalidate()
}

func (a *TaskManagerApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	a.sample()
	a.pushHistory()
	a.readLoad()
	d.Invalidate()
}

func (a *TaskManagerApp) sample() {
	a.sampleKeep()
}

func (a *TaskManagerApp) readLoad() {
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			a.loadAvg = strings.Join(f[:3], " ")
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
				a.uptime = uptimeText(int64(secs))
			}
		}
	}
}

func uptimeText(secs int64) string {
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	if d > 0 {
		return fmt.Sprintf("%d 天 %d 小时 %d 分", d, h, m)
	}
	return fmt.Sprintf("%d 小时 %d 分", h, m)
}

func (a *TaskManagerApp) pushHistory() {
	copy(a.cpuHist[:], a.cpuHist[1:])
	a.cpuHist[len(a.cpuHist)-1] = a.cpuPct
	copy(a.memHist[:], a.memHist[1:])
	a.memHist[len(a.memHist)-1] = a.memPct
	pushU64(&a.rxHist, a.netRateRx)
	pushU64(&a.txHist, a.netRateTx)
	pushU64(&a.dRHist, a.diskRateR)
	pushU64(&a.dWHist, a.diskRateW)
}

func pushU64(arr *[60]uint64, v uint64) {
	copy(arr[:], arr[1:])
	arr[len(arr)-1] = v
}

// sampleKeep refreshes gauges and the process list. It keeps the selected PID
// across re-sorts so a refresh does not jump to another process.
func (a *TaskManagerApp) sampleKeep() {
	selID := a.selID
	if selID < 0 && a.sel >= 0 && a.sel < len(a.procs) {
		selID = a.procs[a.sel].pid
	}

	var dtTotal uint64
	if total, idle, ok := readCPUStat(); ok {
		if a.lastTotal > 0 && total > a.lastTotal {
			dtTotal = total - a.lastTotal
			di := idle
			if idle >= a.lastIdle {
				di = idle - a.lastIdle
			} else {
				di = 0
			}
			if di > dtTotal {
				di = dtTotal
			}
			pct := int((dtTotal - di) * 100 / dtTotal)
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
	a.sampleCounters()

	procs, err := listProcesses()
	if err != nil {
		a.lastErr = err.Error()
	} else {
		a.lastErr = ""
		ncpu := runtime.NumCPU()
		if ncpu < 1 {
			ncpu = 1
		}
		newTicks := make(map[int]uint64, len(procs))
		for i := range procs {
			p := &procs[i]
			if dtTotal > 0 {
				if prev, ok := a.lastProcTicks[p.pid]; ok && p.ticks >= prev {
					p.cpuPct = float64(p.ticks-prev) * float64(ncpu) * 100 / float64(dtTotal)
					if p.cpuPct > 999 {
						p.cpuPct = 999
					}
				}
			}
			newTicks[p.pid] = p.ticks
		}
		a.lastProcTicks = newTicks
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
	a.buildOrder()
	a.scroll.Clamp()
}

func (a *TaskManagerApp) sampleCounters() {
	rx, tx, okNet := readNetDev()
	dr, dw, okDisk := readDiskStats()
	now := time.Now()
	if okNet && okDisk {
		if a.counters {
			secs := now.Sub(a.lastCounters).Seconds()
			if secs > 0.2 {
				if rx >= a.lastNetRx {
					a.netRateRx = uint64(float64(rx-a.lastNetRx) / secs)
				}
				if tx >= a.lastNetTx {
					a.netRateTx = uint64(float64(tx-a.lastNetTx) / secs)
				}
				if dr >= a.lastDiskR {
					a.diskRateR = uint64(float64(dr-a.lastDiskR) / secs)
				}
				if dw >= a.lastDiskW {
					a.diskRateW = uint64(float64(dw-a.lastDiskW) / secs)
				}
			}
		}
		a.lastNetRx, a.lastNetTx = rx, tx
		a.lastDiskR, a.lastDiskW = dr, dw
		a.lastCounters = now
		a.counters = true
	}
}

func (a *TaskManagerApp) lessProc(i, j int) bool {
	x, y := a.procs[i], a.procs[j]
	var less bool
	switch a.sortCol {
	case 1:
		less = strings.ToLower(x.name) < strings.ToLower(y.name)
	case 2:
		less = x.rssKB < y.rssKB
	case 3:
		less = x.cpuPct < y.cpuPct
	default:
		less = x.pid < y.pid
	}
	if a.sortDesc {
		return !less
	}
	return less
}

func (a *TaskManagerApp) sortProcs() {
	sort.SliceStable(a.procs, func(i, j int) bool { return a.lessProc(i, j) })
}

func (a *TaskManagerApp) buildOrder() {
	a.order = a.order[:0]
	if !a.tree {
		for i := range a.procs {
			a.order = append(a.order, taskRow{idx: i})
		}
		a.kids = map[int]int{}
		a.scroll.Total = len(a.order)
		a.scroll.Clamp()
		return
	}
	byPID := make(map[int]int, len(a.procs))
	for i, p := range a.procs {
		byPID[p.pid] = i
	}
	children := make(map[int][]int, len(a.procs))
	var roots []int
	for i, p := range a.procs {
		if p.ppid > 0 && p.ppid != p.pid {
			if _, ok := byPID[p.ppid]; ok {
				children[p.ppid] = append(children[p.ppid], i)
				continue
			}
		}
		roots = append(roots, i)
	}
	a.kids = make(map[int]int, len(children))
	for pid, kids := range children {
		a.kids[pid] = len(kids)
	}
	sortIdx := func(list []int) {
		sort.SliceStable(list, func(x, y int) bool { return a.lessProc(list[x], list[y]) })
	}
	sortIdx(roots)
	for _, kids := range children {
		sortIdx(kids)
	}
	var walk func(i, depth int, last bool)
	walk = func(i, depth int, last bool) {
		a.order = append(a.order, taskRow{idx: i, depth: depth, last: last})
		pid := a.procs[i].pid
		if v, ok := a.expanded[pid]; ok && !v {
			return
		}
		kids := children[pid]
		for k, child := range kids {
			walk(child, depth+1, k == len(kids)-1)
		}
	}
	for k, root := range roots {
		walk(root, 0, k == len(roots)-1)
	}
	a.scroll.Total = len(a.order)
	a.scroll.Clamp()
}

func (a *TaskManagerApp) rowPosition(idx int) int {
	for pos, row := range a.order {
		if row.idx == idx {
			return pos
		}
	}
	return -1
}

func (a *TaskManagerApp) ensureSel() {
	if a.sel < 0 {
		return
	}
	pos := a.rowPosition(a.sel)
	if pos < 0 {
		return
	}
	a.ensurePos(pos)
}

func (a *TaskManagerApp) ensurePos(pos int) {
	rows := a.scroll.Visible
	if rows < 1 {
		rows = 1
	}
	if pos < a.scroll.Pos {
		a.scroll.Pos = pos
	}
	if pos >= a.scroll.Pos+rows {
		a.scroll.Pos = pos - rows + 1
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
	ppid, err0 := strconv.Atoi(fields[1])
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	rss, err3 := strconv.ParseUint(fields[21], 10, 64)
	if err0 != nil || err1 != nil || err2 != nil || err3 != nil {
		return p, false
	}
	p.ppid = ppid
	p.ticks = utime + stime
	p.rssKB = rss
	return p, true
}

func readNetDev() (rx, tx uint64, ok bool) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n")[2:] {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if strings.TrimSpace(name) == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, e1 := strconv.ParseUint(f[0], 10, 64)
		t, e2 := strconv.ParseUint(f[8], 10, 64)
		if e1 == nil && e2 == nil {
			rx += r
			tx += t
		}
	}
	return rx, tx, true
}

func readDiskStats() (read, write uint64, ok bool) {
	b, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		name := f[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "sr") {
			continue
		}
		// Only whole disks: partitions repeat the same counters.
		last := name[len(name)-1]
		if last >= '0' && last <= '9' {
			if !strings.HasPrefix(name, "nvme") && !strings.HasPrefix(name, "mmcblk") {
				continue
			}
			if strings.HasPrefix(name, "nvme") && strings.Contains(name, "p") {
				continue
			}
			if strings.HasPrefix(name, "mmcblk") && strings.Contains(name, "p") {
				continue
			}
		}
		rs, e1 := strconv.ParseUint(f[5], 10, 64)
		ws, e2 := strconv.ParseUint(f[9], 10, 64)
		if e1 == nil && e2 == nil {
			read += rs * 512
			write += ws * 512
		}
	}
	return read, write, true
}

func (a *TaskManagerApp) columns(innerW int) (pidW, nameW, cpuW, memW, stateW int) {
	scrollW := 16
	pidW, cpuW, memW, stateW = 64, 58, 88, 46
	nameW = innerW - scrollW - pidW - cpuW - memW - stateW - 4
	if nameW < 90 {
		nameW = 90
	}
	return
}

func (a *TaskManagerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	a.drawTabs(c, f, r)
	if a.tab == 1 {
		a.paintPerformance(c, f, r)
		return
	}

	gaugeW := (r.W - 36) / 2
	if gaugeW < 120 {
		gaugeW = r.W - 24
	}
	cpuRow := ui.Rect{r.X + 12, r.Y + 38, gaugeW, 22}
	f.Text(c, cpuRow.X, cpuRow.Y, "CPU 使用率", ui.ColText)
	f.TextRight(c, cpuRow.Right(), cpuRow.Y, fmt.Sprintf("%d%%", a.cpuPct), ui.ColText)
	drawGauge(c, ui.Rect{cpuRow.X, cpuRow.Y + 18, cpuRow.W, 7}, a.cpuPct, gaugeColor(a.cpuPct))
	memRow := ui.Rect{r.X + gaugeW + 24, r.Y + 38, gaugeW, 22}
	f.Text(c, memRow.X, memRow.Y, "内存使用率", ui.ColText)
	f.TextRight(c, memRow.Right(), memRow.Y, fmt.Sprintf("%d%%", a.memPct), ui.ColText)
	drawGauge(c, ui.Rect{memRow.X, memRow.Y + 18, memRow.W, 7}, a.memPct, gaugeColor(a.memPct))

	toolbarY := r.Y + 72
	a.btnEnd = ui.Rect{r.X + 12, toolbarY, 104, 26}
	a.btnRefresh = ui.Rect{a.btnEnd.Right() + 8, toolbarY, 72, 26}
	a.btnTree = ui.Rect{a.btnRefresh.Right() + 8, toolbarY, 88, 26}
	ui.Button(c, a.btnEnd, "结束任务", a.press == "end", a.sel >= 0)
	ui.Button(c, a.btnRefresh, "刷新", a.press == "refresh", true)
	ui.Button(c, a.btnTree, "进程树", a.tree, true)
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
	c.FillRect(a.listR, ui.ColField)
	c.Sunken(a.listR)

	inner := a.listR.Inset(2)
	scrollW := 16
	pidW, nameW, cpuW, memW, stateW := a.columns(inner.W)
	heads := []string{"PID", "进程名", "CPU", "内存", "状态"}
	widths := []int{pidW, nameW, cpuW, memW, stateW}
	hx := a.headerR.X + 4
	for i, h := range heads {
		col := ui.ColText
		if a.sortCol == i && i < 4 {
			h = h + " ▾"
		}
		f.Text(c, hx, a.headerR.Y+3, h, col)
		hx += widths[i]
	}
	rows := inner.H / 21
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.order)
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
	for i := a.scroll.Pos; i < len(a.order) && y+20 <= inner.Bottom(); i++ {
		p := a.procs[a.order[i].idx]
		rr := ui.Rect{inner.X, y, inner.W - scrollW, 20}
		if a.order[i].idx == a.sel {
			c.FillRect(rr, ui.ColNavy)
		}
		col := ui.ColText
		if a.order[i].idx == a.sel {
			col = ui.ColWhite
		}
		x := rr.X + 4
		f.Text(c, x, rr.Y+2, strconv.Itoa(p.pid), col)
		x += pidW
		name := p.name
		if a.tree {
			row := a.order[i]
			if row.depth > 0 {
				glyph := "|- "
				if row.last {
					glyph = "`- "
				}
				name = strings.Repeat("   ", row.depth-1) + glyph + name
			}
			if a.kids[p.pid] > 0 {
				if v, ok := a.expanded[p.pid]; ok && !v {
					name = "+ " + name
				} else {
					name = "- " + name
				}
			}
		}
		f.TextClip(c, x, rr.Y+2, nameW-6, name, col)
		x += nameW
		f.TextRight(c, x+cpuW-8, rr.Y+2, fmt.Sprintf("%.1f%%", p.cpuPct), col)
		x += cpuW
		f.TextRight(c, x+memW-8, rr.Y+2, memText(p.rssKB), col)
		x += memW
		f.Text(c, x, rr.Y+2, procStateText(p.state), col)
		y += 21
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H})

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	status := "右键进程可结束、限制 CPU 或网络；双击可折叠进程树。"
	if a.sel >= 0 && a.sel < len(a.procs) {
		p := a.procs[a.sel]
		status = fmt.Sprintf("PID %d  %s  ·  %s  ·  CPU %.1f%%", p.pid, p.name, memText(p.rssKB), p.cpuPct)
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, status, ui.ColText)
}

func (a *TaskManagerApp) drawTabs(c *ui.Canvas, f *ui.Font, r ui.Rect) {
	a.tabProcBtn = ui.Rect{r.X + 8, r.Y + 6, 84, 24}
	a.tabPerfBtn = ui.Rect{a.tabProcBtn.Right() + 4, r.Y + 6, 84, 24}
	ui.Button(c, a.tabProcBtn, "进程", a.tab == 0, true)
	ui.Button(c, a.tabPerfBtn, "性能", a.tab == 1, true)
	if a.uptime != "" || a.loadAvg != "" {
		f.TextRight(c, r.Right()-12, r.Y+10, "负载 "+a.loadAvg+"  ·  运行 "+a.uptime, ui.ColShadow)
	}
}

func (a *TaskManagerApp) paintPerformance(c *ui.Canvas, f *ui.Font, r ui.Rect) {
	area := ui.Rect{r.X + 10, r.Y + 36, r.W - 20, r.Bottom() - 62}
	gap := 8
	pw := (area.W - gap) / 2
	ph := (area.H - gap) / 2
	cpuR := ui.Rect{area.X, area.Y, pw, ph}
	memR := ui.Rect{area.X + pw + gap, area.Y, pw, ph}
	netR := ui.Rect{area.X, area.Y + ph + gap, pw, ph}
	diskR := ui.Rect{area.X + pw + gap, area.Y + ph + gap, pw, ph}

	cpuData := intsToFloats(a.cpuHist[:])
	memData := intsToFloats(a.memHist[:])
	a.drawChart(c, f, cpuR, "CPU 使用率", fmt.Sprintf("%d%%", a.cpuPct), []chartSeries{
		{data: cpuData, color: 0x00FF66, label: "CPU"},
	}, 100)
	a.drawChart(c, f, memR, "内存使用率", fmt.Sprintf("%d%%  %s / %s", a.memPct, memText(a.memUsedKB), memText(a.memTotal)), []chartSeries{
		{data: memData, color: 0x40C0FF, label: "内存"},
	}, 100)
	netMax := autoMax(a.rxHist[:], a.txHist[:])
	a.drawChart(c, f, netR, "网络", fmt.Sprintf("↓ %s/s  ↑ %s/s", rateText(a.netRateRx), rateText(a.netRateTx)), []chartSeries{
		{data: u64sToFloats(a.rxHist[:]), color: 0x00E0FF, label: "接收"},
		{data: u64sToFloats(a.txHist[:]), color: 0xFF7070, label: "发送"},
	}, netMax)
	diskMax := autoMax(a.dRHist[:], a.dWHist[:])
	a.drawChart(c, f, diskR, "磁盘 I/O", fmt.Sprintf("读 %s/s  写 %s/s", rateText(a.diskRateR), rateText(a.diskRateW)), []chartSeries{
		{data: u64sToFloats(a.dRHist[:]), color: 0xFFFF60, label: "读"},
		{data: u64sToFloats(a.dWHist[:]), color: 0xFFA040, label: "写"},
	}, diskMax)

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	status := fmt.Sprintf("%d 个进程 · 60 秒历史 · 负载 %s · 运行 %s", len(a.procs), a.loadAvg, a.uptime)
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, status, ui.ColText)
}

type chartSeries struct {
	data  []float64
	color uint32
	label string
}

func intsToFloats(in []int) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(v)
	}
	return out
}

func u64sToFloats(in []uint64) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(v)
	}
	return out
}

func autoMax(a, b []uint64) float64 {
	var m uint64
	for _, v := range a {
		if v > m {
			m = v
		}
	}
	for _, v := range b {
		if v > m {
			m = v
		}
	}
	if m < 1024 {
		m = 1024
	}
	return float64(m) * 1.15
}

func rateText(v uint64) string {
	switch {
	case v >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(v)/(1024*1024))
	case v >= 1024:
		return fmt.Sprintf("%.1f KB", float64(v)/1024)
	default:
		return fmt.Sprintf("%d B", v)
	}
}

func (a *TaskManagerApp) drawChart(c *ui.Canvas, f *ui.Font, r ui.Rect, title, value string, series []chartSeries, max float64) {
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	head := ui.Rect{r.X + 2, r.Y + 2, r.W - 4, 22}
	c.FillRect(head, ui.ColFace)
	f.TextBold(c, head.X+5, head.Y+2, title, ui.ColText)
	f.TextRight(c, head.Right()-5, head.Y+2, value, ui.ColText)
	lx := head.Right() - 5
	for i := len(series) - 1; i >= 0; i-- {
		s := series[i]
		lw := f.Width(s.label) + 14
		lx -= lw
		f.Text(c, lx+8, head.Y+2, s.label, ui.ColText)
		c.FillRect(ui.Rect{lx, head.Y + 8, 6, 3}, s.color)
	}

	plot := ui.Rect{r.X + 4, head.Bottom() + 1, r.W - 8, r.Bottom() - head.Bottom() - 5}
	if plot.H < 20 {
		return
	}
	c.FillRect(plot, 0x000000)
	c.Frame(plot, ui.ColShadow)
	inner := ui.Rect{plot.X + 1, plot.Y + 1, plot.W - 2, plot.H - 2}
	for i := 1; i < 5; i++ {
		y := inner.Y + inner.H*i/5
		for x := inner.X; x < inner.Right(); x += 4 {
			c.Set(x, y, 0x005020)
		}
	}
	for i := 1; i < 6; i++ {
		x := inner.X + inner.W*i/6
		for y := inner.Y; y < inner.Bottom(); y += 4 {
			c.Set(x, y, 0x005020)
		}
	}
	if max <= 0 {
		max = 1
	}
	for _, s := range series {
		a.drawSeries(c, inner, s, max)
	}
	// Baseline and scale markers
	for x := inner.X; x < inner.Right(); x++ {
		c.Set(x, inner.Bottom()-1, 0x007030)
	}
	f.Text(c, inner.X+2, inner.Y+1, fmt.Sprintf("%.0f", max), 0x60A060)
}

func (a *TaskManagerApp) drawSeries(c *ui.Canvas, inner ui.Rect, s chartSeries, max float64) {
	n := len(s.data)
	if n < 2 || inner.W < 2 {
		return
	}
	prevX, prevY := -1, -1
	for x := 0; x < inner.W; x++ {
		src := x * (n - 1) / (inner.W - 1)
		v := s.data[src]
		if v < 0 {
			v = 0
		}
		if v > max {
			v = max
		}
		y := inner.Bottom() - 1 - int(v*float64(inner.H-2)/max)
		if prevX >= 0 {
			drawSegment(c, inner.X+prevX, prevY, inner.X+x, y, s.color)
		}
		prevX, prevY = x, y
	}
}

func drawSegment(c *ui.Canvas, x0, y0, x1, y1 int, col uint32) {
	dx := x1 - x0
	dy := y1 - y0
	if dx == 0 {
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		for y := y0; y <= y1; y++ {
			c.Set(x0, y, col)
		}
		return
	}
	for x := x0; x <= x1; x++ {
		y := y0 + dy*(x-x0)/dx
		c.Set(x, y, col)
	}
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
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
				d.Invalidate()
			}
		}
		return
	}
	if ev.Kind == MouseMove {
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
				d.Invalidate()
			}
		}
		return
	}
	if ev.Kind == MouseUp {
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
				d.Invalidate()
			}
		}
		p := a.press
		a.press = ""
		if ev.Button == 1 {
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
			case "tree":
				if a.btnTree.Contains(ev.X, ev.Y) {
					a.tree = !a.tree
					a.buildOrder()
					d.Invalidate()
				}
			}
		}
		return
	}
	if ev.Button == 3 {
		if ev.Kind == MouseDown && a.tab == 0 && a.listR.Contains(ev.X, ev.Y) {
			if idx := a.rowAt(ev.X, ev.Y); idx >= 0 {
				a.sel = idx
				a.selID = a.procs[idx].pid
				d.Invalidate()
				a.openProcMenu(idx, ev.X, ev.Y)
			}
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.tabProcBtn.Contains(ev.X, ev.Y) {
			a.tab = 0
			d.Invalidate()
			return
		}
		if a.tabPerfBtn.Contains(ev.X, ev.Y) {
			a.tab = 1
			d.Invalidate()
			return
		}
		if a.tab != 0 {
			return
		}
		switch {
		case a.btnEnd.Contains(ev.X, ev.Y):
			a.press = "end"
		case a.btnRefresh.Contains(ev.X, ev.Y):
			a.press = "refresh"
		case a.btnTree.Contains(ev.X, ev.Y):
			a.press = "tree"
		case a.headerR.Contains(ev.X, ev.Y):
			pidW, nameW, cpuW, memW, _ := a.columns(a.headerR.W)
			x := ev.X - a.headerR.X - 4
			switch {
			case x >= 0 && x < pidW:
				a.setSort(0)
			case x >= pidW && x < pidW+nameW:
				a.setSort(1)
			case x >= pidW+nameW && x < pidW+nameW+cpuW:
				a.setSort(3)
			case x >= pidW+nameW+cpuW && x < pidW+nameW+cpuW+memW:
				a.setSort(2)
			}
		case a.listR.Contains(ev.X, ev.Y):
			pos := a.rowPosAt(ev.X, ev.Y)
			if pos >= 0 {
				idx := a.order[pos].idx
				p := a.procs[idx]
				double := time.Since(a.lastClick) < 450*time.Millisecond && a.lastSel == idx && a.lastClickPos == pos
				a.sel = idx
				a.selID = p.pid
				if double && a.tree && a.kids[p.pid] > 0 {
					a.toggleExpand(p.pid)
					double = false
				}
				a.lastClick = time.Now()
				a.lastSel = idx
				a.lastClickPos = pos
				_ = double
				d.Invalidate()
			}
		}
	}
}

func (a *TaskManagerApp) toggleExpand(pid int) {
	if a.kids[pid] <= 0 {
		return
	}
	if v, ok := a.expanded[pid]; ok && !v {
		a.expanded[pid] = true
	} else {
		a.expanded[pid] = false
	}
	a.buildOrder()
}

func (a *TaskManagerApp) rowPosAt(x, y int) int {
	inner := a.listR.Inset(2)
	if y < inner.Y {
		return -1
	}
	row := (y - inner.Y) / 21
	pos := a.scroll.Pos + row
	if pos >= 0 && pos < len(a.order) && y < inner.Bottom() {
		return pos
	}
	return -1
}

func (a *TaskManagerApp) rowAt(x, y int) int {
	pos := a.rowPosAt(x, y)
	if pos < 0 {
		return -1
	}
	return a.order[pos].idx
}

func (a *TaskManagerApp) openProcMenu(idx int, x, y int) {
	if idx < 0 || idx >= len(a.procs) {
		return
	}
	p := a.procs[idx]
	limitCPU := func(pct int) {
		a.d.MessageBox("限制 CPU",
			fmt.Sprintf("将进程“%s”（PID %d）的 CPU 使用限制为 %d%%？\n\n使用 cgroup v2 的 cpu.max 实现。", p.name, p.pid, pct),
			IconWarning, []string{"应用限制", "取消"}, func(i int) {
				if i != 0 {
					return
				}
				if err := setCPULimit(p.pid, pct); err != nil {
					a.d.MessageBox("限制 CPU", "无法应用 cgroup 限制：\n"+err.Error(), IconError, []string{"确定"}, nil)
					return
				}
				a.d.Notify("系统监视器", fmt.Sprintf("已将 %s 限制为 %d%% CPU", p.name, pct))
			})
	}
	items := []ctxItem{
		{label: "结束任务", icon: ui.IcoNuke, enabled: p.pid > 1 && p.pid != os.Getpid(), action: func() { a.endSelected(a.d) }},
		{label: "结束进程树", icon: ui.IcoNuke, enabled: p.pid > 1 && p.pid != os.Getpid(), action: func() { a.endTree(p) }},
		{sep: true},
		{label: "CPU 限制为 10%…", icon: ui.IcoMonitor, enabled: p.pid > 1, action: func() { limitCPU(10) }},
		{label: "CPU 限制为 25%…", icon: ui.IcoMonitor, enabled: p.pid > 1, action: func() { limitCPU(25) }},
		{label: "解除 CPU 限制", enabled: p.pid > 1, action: func() {
			if err := clearCPULimit(p.pid); err != nil {
				a.d.MessageBox("限制 CPU", "无法解除限制：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.d.Notify("系统监视器", fmt.Sprintf("已解除 %s 的 CPU 限制", p.name))
		}},
		{sep: true},
		{label: "整机限速 1 Mbps (tc)…", icon: ui.IcoNetwork, action: func() { a.applyNetLimit() }},
		{label: "解除整机限速", icon: ui.IcoNetwork, action: func() {
			if err := clearNetLimit(); err != nil {
				a.d.MessageBox("网络限速", "无法解除限速：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.d.Notify("系统监视器", "已解除 tc 网络限速")
		}},
		{sep: true},
		{label: fmt.Sprintf("PID %d  ·  %s", p.pid, p.name), enabled: false},
	}
	a.d.openContext(items, x, y)
}

func (a *TaskManagerApp) applyNetLimit() {
	a.d.MessageBox("网络限速",
		"Linux 无法用 tc 精确限制单个进程的带宽。\n\n"+
			"此操作会对当前默认路由网卡应用 1 Mbps 的 tbf 整机限速，\n"+
			"所有应用共享该带宽。可随时右键“解除整机限速”。",
		IconWarning, []string{"应用 1 Mbps", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if err := applyNetLimit(); err != nil {
				a.d.MessageBox("网络限速", "tc 执行失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.d.Notify("网络限速", "已对默认网卡应用 1 Mbps 限速")
		})
}

func (a *TaskManagerApp) endTree(p taskProc) {
	children := map[int][]int{}
	for i, c := range a.procs {
		if c.ppid > 0 && c.ppid != c.pid {
			children[c.ppid] = append(children[c.ppid], i)
		}
	}
	var pids []int
	var walk func(pid int)
	seen := map[int]bool{}
	walk = func(pid int) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		for _, ci := range children[pid] {
			walk(a.procs[ci].pid)
		}
		pids = append(pids, pid)
	}
	walk(p.pid)
	a.d.MessageBox("结束进程树",
		fmt.Sprintf("确实要结束“%s”（PID %d）及其 %d 个后代进程吗？", p.name, p.pid, len(pids)-1),
		IconWarning, []string{"结束进程树", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			failed := 0
			for _, pid := range pids {
				if pid <= 1 || pid == os.Getpid() {
					continue
				}
				if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
					failed++
				}
			}
			if failed > 0 {
				a.d.MessageBox("结束进程树", fmt.Sprintf("有 %d 个进程无法结束。", failed), IconWarning, []string{"确定"}, nil)
			}
			a.sampleKeep()
			a.d.Invalidate()
		})
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
		if a.tab == 0 {
			a.endSelected(d)
			return true
		}
	case "Up":
		if a.tab != 0 || a.sel < 0 {
			return false
		}
		pos := a.rowPosition(a.sel)
		if pos > 0 {
			a.sel = a.order[pos-1].idx
			a.selID = a.procs[a.sel].pid
			a.ensurePos(pos - 1)
			d.Invalidate()
		}
		return true
	case "Down":
		if a.tab != 0 || a.sel < 0 {
			return false
		}
		pos := a.rowPosition(a.sel)
		if pos >= 0 && pos < len(a.order)-1 {
			a.sel = a.order[pos+1].idx
			a.selID = a.procs[a.sel].pid
			a.ensurePos(pos + 1)
			d.Invalidate()
		}
		return true
	}
	return false
}

func (a *TaskManagerApp) Close() {}

const cgroupRoot = "/sys/fs/cgroup"

func setCPULimit(pid, pct int) error {
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		return fmt.Errorf("系统未启用 cgroup v2")
	}
	_ = os.WriteFile(filepath.Join(cgroupRoot, "cgroup.subtree_control"), []byte("+cpu"), 0o644)
	dir := filepath.Join(cgroupRoot, fmt.Sprintf("win95-limit-%d", pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	quota := pct * 1000
	if err := os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(fmt.Sprintf("%d 100000", quota)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	return nil
}

func clearCPULimit(pid int) error {
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.procs")); err != nil {
		return fmt.Errorf("系统未启用 cgroup v2")
	}
	if err := os.WriteFile(filepath.Join(cgroupRoot, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(cgroupRoot, fmt.Sprintf("win95-limit-%d", pid)))
	return nil
}

func defaultRouteIface() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if f[1] == "00000000" {
			return f[0]
		}
	}
	return ""
}

func applyNetLimit() error {
	iface := defaultRouteIface()
	if iface == "" {
		return fmt.Errorf("找不到默认路由网卡")
	}
	if err := ensureTool("tc", "iproute2-tc"); err != nil {
		return err
	}
	cmd := exec.Command("tc", "qdisc", "replace", "dev", iface, "root", "tbf",
		"rate", "1mbit", "burst", "32kbit", "latency", "400ms")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s：%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func clearNetLimit() error {
	if _, err := exec.LookPath("tc"); err != nil {
		return nil
	}
	iface := defaultRouteIface()
	if iface == "" {
		return fmt.Errorf("找不到默认路由网卡")
	}
	cmd := exec.Command("tc", "qdisc", "del", "dev", iface, "root")
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "No such file") {
		return fmt.Errorf("%s：%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
