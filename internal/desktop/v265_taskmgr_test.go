package desktop

import (
	"strings"
	"testing"

	"win95linux/internal/ui"
)

func TestParseProcStatParent(t *testing.T) {
	fields := []string{
		"S", "42", "2", "3", "4", "5", "6", "7", "8", "9",
		"10", "11", "12", "13", "14", "15", "16", "17", "18", "19",
		"20", "21",
	}
	p, ok := parseProcStat("777 (worker) " + strings.Join(fields, " "))
	if !ok {
		t.Fatal("valid stat record rejected")
	}
	if p.ppid != 42 || p.ticks != 23 || p.rssKB != 21 {
		t.Fatalf("ppid=%d ticks=%d rss=%d", p.ppid, p.ticks, p.rssKB)
	}
}

func TestTaskTreeOrderAndCollapse(t *testing.T) {
	a := &TaskManagerApp{
		procs: []taskProc{
			{pid: 1, ppid: 0, name: "init"},
			{pid: 10, ppid: 1, name: "shell"},
			{pid: 11, ppid: 10, name: "grep"},
			{pid: 12, ppid: 10, name: "cat"},
			{pid: 20, ppid: 1, name: "logger"},
		},
		tree:     true,
		expanded: map[int]bool{},
		sortCol:  0,
	}
	a.buildOrder()
	if len(a.order) != 5 {
		t.Fatalf("tree rows = %d, want 5", len(a.order))
	}
	if a.order[0].depth != 0 || a.order[1].depth != 1 || a.order[2].depth != 2 {
		t.Fatalf("depths = %d,%d,%d", a.order[0].depth, a.order[1].depth, a.order[2].depth)
	}
	if a.kids[1] != 2 || a.kids[10] != 2 {
		t.Fatalf("kids = %+v", a.kids)
	}
	if a.procs[a.order[1].idx].pid != 10 {
		t.Fatalf("first child = %d, want 10", a.procs[a.order[1].idx].pid)
	}

	a.expanded[1] = false
	a.buildOrder()
	if len(a.order) != 1 || a.procs[a.order[0].idx].pid != 1 {
		t.Fatalf("collapsed root rows = %+v", a.order)
	}
	a.expanded[1] = true
	a.expanded[10] = false
	a.buildOrder()
	if len(a.order) != 3 {
		t.Fatalf("collapsed subtree rows = %d, want 3", len(a.order))
	}

	a.tree = false
	a.buildOrder()
	if len(a.order) != 5 || a.order[3].depth != 0 {
		t.Fatalf("flat rows = %+v", a.order)
	}
	for pos, row := range a.order {
		if row.depth != 0 {
			t.Fatalf("flat row %d has depth %d", pos, row.depth)
		}
	}
}

func TestTaskManagerPaintTabs(t *testing.T) {
	d := testDesktop(t)
	a := &TaskManagerApp{
		d:        d,
		procs:    []taskProc{{pid: 1, name: "init", state: 'S', rssKB: 2048, cpuPct: 1.5}, {pid: 7, name: "app", ppid: 1, state: 'R', rssKB: 65536, cpuPct: 22}},
		tree:     true,
		expanded: map[int]bool{},
		sortCol:  0,
		sel:      0,
		selID:    1,
		cpuPct:   37,
		memPct:   58,
		loadAvg:  "0.42 0.31 0.20",
		uptime:   "1 小时 2 分",
	}
	a.buildOrder()
	c := ui.NewCanvas(800, 560)
	a.Paint(d, nil, c, ui.Rect{0, 0, 800, 560})
	if a.tabProcBtn.W == 0 || a.listR.H == 0 {
		t.Fatal("paint did not lay out tabs/list")
	}
	a.tab = 1
	a.rxHist[59] = 1024
	a.txHist[59] = 2048
	a.cpuHist[59] = 37
	a.memHist[59] = 58
	a.Paint(d, nil, c, ui.Rect{0, 0, 800, 560})
	if a.tabPerfBtn.W == 0 {
		t.Fatal("performance tab was not laid out")
	}
	drawn := false
	for _, px := range c.Pix {
		if px == 0x00FF66 {
			drawn = true
			break
		}
	}
	if !drawn {
		t.Fatal("CPU chart line was not drawn")
	}
}

func TestRateAndUptimeFormatting(t *testing.T) {
	if got := rateText(512); got != "512 B" {
		t.Fatalf("rateText(512) = %q", got)
	}
	if got := rateText(2048); got != "2.0 KB" {
		t.Fatalf("rateText(2048) = %q", got)
	}
	if got := rateText(3 * 1024 * 1024); got != "3.0 MB" {
		t.Fatalf("rateText(3MB) = %q", got)
	}
	if got := uptimeText(90061); got != "1 天 1 小时 1 分" {
		t.Fatalf("uptimeText = %q", got)
	}
	if got := autoMax([]uint64{10, 20}, []uint64{5}); got != 1024*1.15 {
		t.Fatalf("autoMax floor = %v", got)
	}
}
