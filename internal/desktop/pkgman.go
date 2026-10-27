package desktop

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"win95linux/internal/ui"
)

const (
	apkBin       = "/sbin/apk"
	apkInstalled = "/lib/apk/db/installed"
	apkCacheDir  = "/var/cache/apk"

	pkgRowH     = 19
	pkgHeaderH  = 20
	pkgMaxItems = 400
	pkgLogLines = 3
)

type pkgEntry struct {
	Name, Version, Desc string
	Installed           bool
}

// PkgManApp is a graphical front end for Alpine's apk package manager.
type PkgManApp struct {
	Base
	d    *Desktop
	win  *Window
	busy bool

	query  []rune
	caret  int
	focus  int // 0 list, 1 search box
	items  []pkgEntry
	sel    int
	scroll VScroll

	installed map[string]pkgEntry
	status    string
	log       []string

	inputR, searchR, refreshR ui.Rect
	listR, scrollR, installR  ui.Rect
	removeR, logR             ui.Rect
	press                     string

	lastClick time.Time
	lastIndex int

	cmdMu sync.Mutex
	cmd   *exec.Cmd
}

func NewPkgManApp(d *Desktop) *PkgManApp {
	a := &PkgManApp{d: d, sel: -1, lastIndex: -1, status: "就绪"}
	a.installed = readInstalledPackages()
	a.showInstalled()
	if apkCacheEmpty() {
		a.appendLog("软件源索引尚未下载，正在获取…")
		a.run("正在下载软件目录", "update")
	}
	return a
}

func (a *PkgManApp) Title() string           { return "软件包管理器" }
func (a *PkgManApp) Icon() ui.IconKind       { return ui.IcoPackage }
func (a *PkgManApp) DefaultSize() (int, int) { return 680, 480 }
func (a *PkgManApp) MinSize() (int, int)     { return 560, 380 }
func (a *PkgManApp) Resizable() bool         { return true }

func (a *PkgManApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "刷新软件目录", Accel: "F5", Enabled: true, Action: func() { a.run("正在下载软件目录", "update") }},
			{Separator: true},
			{Label: "关闭", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "操作(A)", Items: []MenuItem{
			{Label: "安装所选软件", Enabled: true, Action: func() { a.installSelected() }},
			{Label: "卸载所选软件", Enabled: true, Action: func() { a.removeSelected() }},
			{Separator: true},
			{Label: "显示已安装软件", Enabled: true, Action: func() { a.query = nil; a.caret = 0; a.showInstalled() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于软件包管理器", Enabled: true, Action: func() {
				d.MessageBox("关于软件包管理器",
					"Windows 95 Linux 软件包管理器\n\n基于 Alpine Linux 的 apk 包管理器，使用 ARM64 官方软件仓库。\n\n安装的软件可以在终端中运行；图形程序需要随系统打包。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *PkgManApp) Close() {
	a.cmdMu.Lock()
	defer a.cmdMu.Unlock()
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
}

func (a *PkgManApp) showInstalled() {
	items := make([]pkgEntry, 0, len(a.installed))
	for _, e := range a.installed {
		e.Installed = true
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	a.items = items
	a.sel = -1
	if len(items) > 0 {
		a.sel = 0
	}
	a.scroll.Pos = 0
	a.scroll.Clamp()
	a.status = fmt.Sprintf("已安装 %d 个软件包", len(items))
	a.d.Invalidate()
}

func (a *PkgManApp) current() *pkgEntry {
	if a.sel < 0 || a.sel >= len(a.items) {
		return nil
	}
	return &a.items[a.sel]
}

func (a *PkgManApp) startSearch() {
	q := strings.TrimSpace(string(a.query))
	if a.busy {
		return
	}
	if q == "" {
		a.showInstalled()
		return
	}
	a.busy = true
	a.status = "正在搜索 " + q + " …"
	a.d.Invalidate()
	go func() {
		stdout, stderr, err := runApk("search", "-d", "-v", "-v", q)
		entries := parseSearchOutput(stdout)
		a.d.Post(func() {
			a.busy = false
			for _, line := range tailLines(stderr, 8) {
				a.appendLog(line)
			}
			if err != nil {
				a.status = "搜索失败，请检查网络后点“刷新”"
				a.appendLog("搜索失败: " + errText(stderr, err))
			} else {
				a.items = a.decorate(entries)
				a.sel = -1
				if len(a.items) > 0 {
					a.sel = 0
					a.status = fmt.Sprintf("找到 %d 个与“%s”相关的软件包", len(a.items), q)
				} else {
					a.status = fmt.Sprintf("没有找到“%s”；软件包名称和描述是英文，请换用英文关键词", q)
				}
				a.scroll.Pos = 0
			}
			a.scroll.Total = len(a.items)
			a.scroll.Clamp()
			a.d.Invalidate()
		})
	}()
}

func (a *PkgManApp) decorate(entries []pkgEntry) []pkgEntry {
	out := make([]pkgEntry, 0, len(entries))
	for _, e := range entries {
		if inst, ok := a.installed[e.Name]; ok {
			e.Installed = true
			e.Version = inst.Version
			if e.Desc == "" {
				e.Desc = inst.Desc
			}
		}
		out = append(out, e)
		if len(out) == pkgMaxItems {
			a.appendLog(fmt.Sprintf("结果过多，仅显示前 %d 项，请输入更精确的关键词。", pkgMaxItems))
			break
		}
	}
	return out
}

func (a *PkgManApp) installSelected() {
	e := a.current()
	if e == nil || a.busy {
		return
	}
	if e.Installed {
		a.status = e.Name + " 已经安装"
		a.d.Invalidate()
		return
	}
	a.runOp("正在安装 "+e.Name, "add", e.Name, "add", e.Name)
}

func (a *PkgManApp) removeSelected() {
	e := a.current()
	if e == nil || a.busy {
		return
	}
	if !e.Installed {
		a.status = e.Name + " 没有安装"
		a.d.Invalidate()
		return
	}
	a.runOp("正在卸载 "+e.Name, "del", e.Name, "del", e.Name)
}

// run executes apk asynchronously and streams its output into the log pane.
func (a *PkgManApp) run(label string, args ...string) {
	a.runOp(label, "", "", args...)
}

// runOp is run plus post-processing for installs and removals. op is "add" or
// "del"; pkg is the package whose desktop shortcuts need refreshing.
func (a *PkgManApp) runOp(label, op, pkg string, args ...string) {
	if a.busy {
		return
	}
	a.busy = true
	a.status = label + " …"
	a.appendLog("$ apk " + strings.Join(args, " "))
	a.d.Invalidate()

	go func() {
		cmd := exec.Command(apkBin, args...)
		cmd.Dir = "/"
		cmd.Env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"LANG=C.UTF-8",
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			a.finish(label, err, "", nil, false)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			a.finish(label, err, "", nil, false)
			return
		}
		a.cmdMu.Lock()
		a.cmd = cmd
		a.cmdMu.Unlock()
		if err := cmd.Start(); err != nil {
			a.cmdMu.Lock()
			a.cmd = nil
			a.cmdMu.Unlock()
			a.finish(label, err, "", nil, false)
			return
		}
		var wg sync.WaitGroup
		var errBuf strings.Builder
		wg.Add(2)
		go func() { defer wg.Done(); a.stream(stdout) }()
		go func() {
			defer wg.Done()
			s := bufio.NewScanner(stderr)
			for s.Scan() {
				line := s.Text()
				errBuf.WriteString(line + "\n")
				a.d.Post(func() { a.appendLog(line) })
			}
		}()
		wg.Wait()
		err = cmd.Wait()
		a.cmdMu.Lock()
		a.cmd = nil
		a.cmdMu.Unlock()

		var extra []string
		if err == nil {
			switch op {
			case "add":
				if made, e := generateDesktopShortcuts(pkg); e != nil {
					extra = append(extra, "创建桌面快捷方式失败: "+e.Error())
				} else {
					for _, p := range made {
						extra = append(extra, "已创建桌面快捷方式: "+filepath.Base(p))
					}
				}
			case "del":
				for _, p := range removeDesktopShortcuts(pkg) {
					extra = append(extra, "已删除桌面快捷方式: "+filepath.Base(p))
				}
			}
		}
		a.finish(label, err, errBuf.String(), extra, op != "")
	}()
}

func (a *PkgManApp) stream(r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		a.d.Post(func() { a.appendLog(line) })
	}
}

func (a *PkgManApp) finish(label string, err error, stderr string, extra []string, refresh bool) {
	a.d.Post(func() {
		a.busy = false
		a.installed = readInstalledPackages()
		switch {
		case err != nil:
			a.status = label + " 失败"
			a.appendLog("失败: " + errText(stderr, err))
		default:
			a.status = label + " 完成"
			a.appendLog(label + " 完成")
		}
		for _, line := range extra {
			a.appendLog(line)
		}
		if refresh {
			a.d.RefreshShortcuts()
		}
		if len(a.query) > 0 {
			a.startSearch()
		} else {
			a.showInstalled()
		}
		a.d.Invalidate()
	})
}

func (a *PkgManApp) appendLog(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	a.log = append(a.log, line)
	if len(a.log) > 200 {
		a.log = a.log[len(a.log)-200:]
	}
	a.d.Invalidate()
}

func (a *PkgManApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	f.Text(c, r.X+8, r.Y+9, "搜索:", ui.ColText)
	a.inputR = ui.Rect{r.X + 46, r.Y + 6, r.W - 46 - 150, 22}
	c.FillRect(a.inputR, ui.ColWhite)
	c.Sunken(a.inputR)
	text := string(a.query)
	if text == "" {
		f.TextClip(c, a.inputR.X+6, a.inputR.Y+3, a.inputR.W-12, "输入软件名称或描述（英文）…", ui.ColTextDis)
	} else {
		f.TextClip(c, a.inputR.X+6, a.inputR.Y+3, a.inputR.W-12, text, ui.ColText)
	}
	if a.focus == 1 {
		cx := a.inputR.X + 6 + f.Width(text[:a.caretBytes()])
		if cx > a.inputR.Right()-4 {
			cx = a.inputR.Right() - 4
		}
		for y := a.inputR.Y + 3; y < a.inputR.Bottom()-3; y++ {
			c.Set(cx, y, ui.ColText)
		}
	}
	a.searchR = ui.Rect{a.inputR.Right() + 6, r.Y + 6, 58, 24}
	ui.Button(c, a.searchR, "搜索", a.press == "search", !a.busy)
	a.refreshR = ui.Rect{a.searchR.Right() + 6, r.Y + 6, 58, 24}
	ui.Button(c, a.refreshR, "刷新", a.press == "refresh", !a.busy)

	// Package list.
	a.listR = ui.Rect{r.X + 8, r.Y + 36, r.W - 16, r.H - 36 - 110}
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	scrollW := 16
	a.scrollR = ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H}
	bodyW := inner.W - scrollW
	verW, stateW := 118, 64
	nameW := bodyW - verW - stateW - 8
	if nameW < 120 {
		nameW = 120
	}

	c.FillRect(ui.Rect{inner.X, inner.Y, bodyW, pkgHeaderH}, ui.ColFace)
	c.HLine(inner.X, inner.Y+pkgHeaderH-1, bodyW, ui.ColShadow)
	f.Text(c, inner.X+4, inner.Y+2, "名称", ui.ColText)
	f.Text(c, inner.X+nameW, inner.Y+2, "版本", ui.ColText)
	f.Text(c, inner.X+nameW+verW, inner.Y+2, "状态", ui.ColText)

	rows := (inner.H - pkgHeaderH - 2) / pkgRowH
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.items)
	a.scroll.Visible = rows
	a.scroll.Clamp()

	y := inner.Y + pkgHeaderH
	for i := a.scroll.Pos; i < len(a.items) && y+pkgRowH <= inner.Bottom(); i++ {
		e := a.items[i]
		row := ui.Rect{inner.X + 1, y, bodyW - 2, pkgRowH - 1}
		fg := ui.ColText
		if i == a.sel {
			c.FillRect(row, ui.ColNavy)
			fg = ui.ColWhite
		}
		f.TextClip(c, row.X+3, row.Y+1, nameW-6, e.Name, fg)
		f.TextClip(c, row.X+nameW, row.Y+1, verW-6, e.Version, fg)
		state, col := "", ui.ColText
		if e.Installed {
			state, col = "已安装", ui.ColGreen
			if i == a.sel {
				col = ui.ColWhite
			}
		}
		f.TextClip(c, row.X+nameW+verW, row.Y+1, stateW-4, state, col)
		y += pkgRowH
	}
	if len(a.items) == 0 {
		f.TextClip(c, inner.X+8, inner.Y+pkgHeaderH+8, bodyW-16, "没有可显示的软件包。", ui.ColTextDis)
	}
	a.scroll.Draw(c, a.scrollR)

	// Details, log and actions.
	detailY := a.listR.Bottom() + 5
	detail := ""
	if e := a.current(); e != nil {
		detail = e.Name + " " + e.Version
		if e.Desc != "" {
			detail += " — " + e.Desc
		}
	}
	f.TextClip(c, r.X+8, detailY, r.W-16, detail, ui.ColText)

	a.logR = ui.Rect{r.X + 8, detailY + 20, r.W - 16, pkgLogLines*16 + 8}
	c.FillRect(a.logR, ui.ColWhite)
	c.Sunken(a.logR)
	lines := a.log
	if len(lines) > pkgLogLines {
		lines = lines[len(lines)-pkgLogLines:]
	}
	for i, line := range lines {
		col := ui.ColText
		if strings.HasPrefix(line, "失败") || strings.HasPrefix(line, "ERROR") {
			col = ui.ColRed
		}
		f.TextClip(c, a.logR.X+4, a.logR.Y+3+i*16, a.logR.W-8, line, col)
	}

	btnY := r.Bottom() - 28
	a.installR = ui.Rect{r.Right() - 8 - 76, btnY, 76, 24}
	a.removeR = ui.Rect{a.installR.X - 6 - 76, btnY, 76, 24}
	e := a.current()
	ui.Button(c, a.installR, "安装(I)", a.press == "install", !a.busy && e != nil && !e.Installed)
	ui.Button(c, a.removeR, "卸载(U)", a.press == "remove", !a.busy && e != nil && e.Installed)
	f.TextClip(c, r.X+8, btnY+5, a.removeR.X-r.X-16, a.status, ui.ColText)
}

func (a *PkgManApp) caretBytes() int {
	n := a.caret
	if n > len(a.query) {
		n = len(a.query)
	}
	if n < 0 {
		n = 0
	}
	return len(string(a.query[:n]))
}

func (a *PkgManApp) rowAt(x, y int) int {
	inner := a.listR.Inset(2)
	if x < inner.X || x >= a.scrollR.X || y < inner.Y+pkgHeaderH {
		return -1
	}
	idx := a.scroll.Pos + (y-inner.Y-pkgHeaderH)/pkgRowH
	if idx < 0 || idx >= len(a.items) {
		return -1
	}
	return idx
}

func (a *PkgManApp) action() {
	e := a.current()
	if e == nil || a.busy {
		return
	}
	if e.Installed {
		a.removeSelected()
	} else {
		a.installSelected()
	}
}

func (a *PkgManApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		if a.listR.Contains(ev.X, ev.Y) {
			a.scroll.ScrollBy(-ev.Wheel)
			d.Invalidate()
		}
		return
	}
	if ev.Button == 3 {
		if ev.Kind == MouseDown {
			if idx := a.rowAt(ev.X, ev.Y); idx >= 0 {
				a.sel = idx
				a.focus = 0
				d.Invalidate()
			}
		}
		return
	}
	if changed, used := a.scroll.Mouse(ev, a.scrollR); used {
		if changed {
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.inputR.Contains(ev.X, ev.Y) {
			a.focus = 1
			a.caret = len(a.query)
			d.Invalidate()
			return
		}
		if a.listR.Contains(ev.X, ev.Y) {
			a.focus = 0
			if idx := a.rowAt(ev.X, ev.Y); idx >= 0 {
				now := time.Now()
				double := idx == a.lastIndex && now.Sub(a.lastClick) < 400*time.Millisecond
				a.sel = idx
				a.lastClick = now
				a.lastIndex = idx
				if double {
					a.action()
				}
			}
			d.Invalidate()
			return
		}
		switch {
		case a.searchR.Contains(ev.X, ev.Y):
			a.press = "search"
		case a.refreshR.Contains(ev.X, ev.Y):
			a.press = "refresh"
		case a.installR.Contains(ev.X, ev.Y):
			a.press = "install"
		case a.removeR.Contains(ev.X, ev.Y):
			a.press = "remove"
		}
		if a.press != "" {
			d.Invalidate()
		}
	case MouseUp:
		press := a.press
		a.press = ""
		switch press {
		case "search":
			if a.searchR.Contains(ev.X, ev.Y) {
				a.startSearch()
			}
		case "refresh":
			if a.refreshR.Contains(ev.X, ev.Y) {
				a.run("正在下载软件目录", "update")
			}
		case "install":
			if a.installR.Contains(ev.X, ev.Y) {
				a.installSelected()
			}
		case "remove":
			if a.removeR.Contains(ev.X, ev.Y) {
				a.removeSelected()
			}
		}
		d.Invalidate()
	}
}

func (a *PkgManApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl {
		return false
	}
	if ev.Rune >= 32 && ev.Rune != 127 {
		a.query = append(a.query, 0)
		copy(a.query[a.caret+1:], a.query[a.caret:])
		a.query[a.caret] = ev.Rune
		a.caret++
		a.focus = 1
		d.Invalidate()
		return true
	}
	switch ev.Name {
	case "Backspace":
		if a.focus == 1 && a.caret > 0 {
			a.query = append(a.query[:a.caret-1], a.query[a.caret:]...)
			a.caret--
			d.Invalidate()
			return true
		}
	case "Delete":
		if a.focus == 1 && a.caret < len(a.query) {
			a.query = append(a.query[:a.caret], a.query[a.caret+1:]...)
			d.Invalidate()
			return true
		}
	case "Left":
		if a.focus == 1 && a.caret > 0 {
			a.caret--
			d.Invalidate()
			return true
		}
	case "Right":
		if a.focus == 1 && a.caret < len(a.query) {
			a.caret++
			d.Invalidate()
			return true
		}
	case "Home":
		if a.focus == 1 {
			a.caret = 0
		} else {
			a.sel = 0
			a.scroll.Pos = 0
		}
		d.Invalidate()
		return true
	case "End":
		if a.focus == 1 {
			a.caret = len(a.query)
		} else if len(a.items) > 0 {
			a.sel = len(a.items) - 1
			a.scroll.Pos = a.sel
			a.scroll.Clamp()
		}
		d.Invalidate()
		return true
	case "Up", "Down", "PgUp", "PgDn":
		if a.focus == 1 && ev.Name == "Up" {
			a.startSearch()
			return true
		}
		step := 1
		switch ev.Name {
		case "Up":
			step = -1
		case "PgUp":
			step = -a.scroll.Visible
		case "PgDn":
			step = a.scroll.Visible
		}
		if len(a.items) > 0 {
			if a.sel < 0 {
				a.sel = 0
			} else {
				a.sel += step
				if a.sel < 0 {
					a.sel = 0
				}
				if a.sel >= len(a.items) {
					a.sel = len(a.items) - 1
				}
			}
			if a.sel < a.scroll.Pos {
				a.scroll.Pos = a.sel
			}
			if a.sel >= a.scroll.Pos+a.scroll.Visible {
				a.scroll.Pos = a.sel - a.scroll.Visible + 1
			}
			a.scroll.Clamp()
		}
		a.focus = 0
		d.Invalidate()
		return true
	case "Enter":
		if a.focus == 1 {
			a.startSearch()
		} else {
			a.action()
		}
		return true
	case "Esc":
		a.query = nil
		a.caret = 0
		a.showInstalled()
		return true
	case "F5":
		a.run("正在下载软件目录", "update")
		return true
	}
	return false
}

// readInstalledPackages parses apk's installed database. The on-disk format is
// a sequence of "Field:value" lines with a blank line between packages.
func readInstalledPackages() map[string]pkgEntry {
	f, err := os.Open(apkInstalled)
	if err != nil {
		return map[string]pkgEntry{}
	}
	defer f.Close()
	return parseInstalledDB(f)
}

func parseInstalledDB(r io.Reader) map[string]pkgEntry {
	out := map[string]pkgEntry{}
	var cur pkgEntry
	s := bufio.NewScanner(r)
	commit := func() {
		if cur.Name != "" {
			out[cur.Name] = cur
		}
		cur = pkgEntry{}
	}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			commit()
			continue
		}
		if len(line) < 2 || line[1] != ':' {
			continue
		}
		val := line[2:]
		switch line[0] {
		case 'P':
			cur.Name = val
		case 'V':
			cur.Version = val
		case 'T':
			cur.Desc = val
		}
	}
	commit()
	return out
}

func apkCacheEmpty() bool {
	ents, err := os.ReadDir(apkCacheDir)
	return err == nil && len(ents) == 0
}

func runApk(args ...string) (string, string, error) {
	cmd := exec.Command(apkBin, args...)
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C.UTF-8",
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// parseSearchOutput understands "apk search -d -v -v" output, which is
// "name-version - description" per line.
func parseSearchOutput(out string) []pkgEntry {
	var list []pkgEntry
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nameVer, desc := line, ""
		if i := strings.Index(line, " - "); i > 0 {
			nameVer = line[:i]
			desc = strings.TrimSpace(line[i+3:])
		}
		name, ver := splitNameVersion(nameVer)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		list = append(list, pkgEntry{Name: name, Version: ver, Desc: desc})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func splitNameVersion(s string) (string, string) {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] != '-' || i+1 >= len(s) {
			continue
		}
		if c := s[i+1]; c >= '0' && c <= '9' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func tailLines(s string, n int) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func errText(stderr string, err error) string {
	if lines := tailLines(stderr, 3); len(lines) > 0 {
		return lines[len(lines)-1]
	}
	if err != nil {
		return err.Error()
	}
	return "未知错误"
}
