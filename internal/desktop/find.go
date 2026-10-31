package desktop

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"win95linux/internal/ui"
)

const maxFindResults = 1000

type findResult struct {
	name  string
	dir   string
	path  string
	isDir bool
	size  int64
}

type FindApp struct {
	Base
	d   *Desktop
	win *Window

	query      []rune
	queryCaret int
	scope      []rune
	scopeCaret int
	focus      int // 0 name, 1 scope, 2 results
	recursive  bool
	hidden     bool

	results []findResult
	sel     int
	scroll  VScroll
	running bool
	done    bool
	stopped bool
	status  string

	token  atomic.Uint64
	stop   atomic.Bool
	closed atomic.Bool

	press     string
	lastClick time.Time
	lastIndex int

	nameR, scopeR, listR                  ui.Rect
	startR, stopR, openR, folderR, closeR ui.Rect
	recurseR, hiddenR                     ui.Rect
}

func NewFindApp(d *Desktop) *FindApp {
	a := &FindApp{d: d, recursive: true, sel: -1, lastIndex: -1, status: "输入名称，然后单击“开始查找”。"}
	a.scope = []rune("/root")
	a.scopeCaret = len(a.scope)
	return a
}

func (d *Desktop) openFind() {
	for _, w := range d.windows {
		if _, ok := w.App.(*FindApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewFindApp(d)
	w := d.Open(a)
	a.win = w
}

func (a *FindApp) Title() string           { return "查找：文件或文件夹" }
func (a *FindApp) Icon() ui.IconKind       { return ui.IcoFolderOpen }
func (a *FindApp) DefaultSize() (int, int) { return 660, 460 }
func (a *FindApp) MinSize() (int, int)     { return 560, 340 }
func (a *FindApp) Resizable() bool         { return true }

func (a *FindApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	labelW := 120
	y := r.Y + 12
	f.Text(c, r.X+12, y+4, "名称(N):", ui.ColText)
	a.nameR = ui.Rect{r.X + labelW, y, r.W - labelW - 12, 24}
	a.drawField(c, f, a.nameR, a.query, a.queryCaret, a.focus == 0)

	y += 32
	f.Text(c, r.X+12, y+4, "搜索范围(L):", ui.ColText)
	a.scopeR = ui.Rect{r.X + labelW, y, r.W - labelW - 12, 24}
	a.drawField(c, f, a.scopeR, a.scope, a.scopeCaret, a.focus == 1)

	y += 32
	a.recurseR = ui.Rect{r.X + labelW, y + 1, 13, 13}
	ui.Checkbox(c, a.recurseR.X, a.recurseR.Y, a.recursive, true)
	f.Text(c, a.recurseR.X+20, a.recurseR.Y-1, "包含子文件夹", ui.ColText)
	a.hiddenR = ui.Rect{r.X + labelW + 160, y + 1, 13, 13}
	ui.Checkbox(c, a.hiddenR.X, a.hiddenR.Y, a.hidden, true)
	f.Text(c, a.hiddenR.X+20, a.hiddenR.Y-1, "包含隐藏文件", ui.ColText)

	y += 22
	a.startR = ui.Rect{r.X + labelW, y, 96, 26}
	a.stopR = ui.Rect{a.startR.Right() + 8, y, 84, 26}
	ui.Button(c, a.startR, "开始查找(S)", a.press == "start", !a.running)
	ui.Button(c, a.stopR, "停止(P)", a.press == "stop", a.running)
	if a.running {
		f.TextClip(c, a.stopR.Right()+12, a.stopR.Y+6, r.Right()-a.stopR.Right()-24, fmt.Sprintf("已找到 %d 个项目…", len(a.results)), ui.ColText)
	} else if a.done {
		f.TextClip(c, a.stopR.Right()+12, a.stopR.Y+6, r.Right()-a.stopR.Right()-24, fmt.Sprintf("找到 %d 个项目", len(a.results)), ui.ColText)
	}

	listTop := y + 38
	a.listR = ui.Rect{r.X + 12, listTop, r.W - 24, r.Bottom() - listTop - 64}
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	scrollW := 16
	headH := 20
	avail := inner.W - scrollW - 2
	sizeW, typeW := 80, 70
	nameW := avail * 35 / 100
	locW := avail - nameW - sizeW - typeW
	if locW < 120 {
		nameW = avail - 120 - sizeW - typeW
		locW = 120
		if nameW < 120 {
			nameW = 120
			locW = avail - nameW - sizeW - typeW
		}
	}
	c.FillRect(ui.Rect{inner.X, inner.Y, inner.W - scrollW, headH}, ui.ColFace)
	c.HLine(inner.X, inner.Y+headH-1, inner.W-scrollW, ui.ColShadow)
	f.Text(c, inner.X+5, inner.Y+3, "名称", ui.ColText)
	f.Text(c, inner.X+nameW, inner.Y+3, "位置", ui.ColText)
	f.Text(c, inner.X+nameW+locW, inner.Y+3, "大小", ui.ColText)
	f.Text(c, inner.X+nameW+locW+sizeW, inner.Y+3, "类型", ui.ColText)

	rows := (inner.H - headH - 4) / 21
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.results)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	if len(a.results) == 0 {
		msg := a.status
		if a.running {
			msg = "正在查找…"
		}
		f.TextClip(c, inner.X+10, inner.Y+headH+8, inner.W-scrollW-20, msg, ui.ColShadow)
	}
	yy := inner.Y + headH + 1
	for i := a.scroll.Pos; i < len(a.results) && yy+21 <= inner.Bottom(); i++ {
		e := a.results[i]
		rr := ui.Rect{inner.X + 1, yy, inner.W - scrollW - 2, 20}
		if i == a.sel {
			c.FillRect(rr, ui.ColNavy)
		}
		kind := ui.IcoFile
		typ := "文件"
		if e.isDir {
			kind = ui.IcoFolder
			typ = "文件夹"
		}
		ui.DrawIcon16(c, kind, rr.X+3, rr.Y+2)
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		f.TextClip(c, rr.X+23, rr.Y+2, nameW-26, e.name, col)
		f.TextClip(c, inner.X+nameW, rr.Y+2, locW-6, e.dir, col)
		if !e.isDir {
			f.Text(c, inner.X+nameW+locW, rr.Y+2, sizeText(e.size), col)
		}
		f.Text(c, inner.X+nameW+locW+sizeW, rr.Y+2, typ, col)
		yy += 21
	}
	a.scroll.Draw(c, a.findScrollRect())

	by := r.Bottom() - 52
	a.openR = ui.Rect{r.X + 12, by, 84, 26}
	a.folderR = ui.Rect{a.openR.Right() + 8, by, 132, 26}
	a.closeR = ui.Rect{r.Right() - 96, by, 84, 26}
	ui.Button(c, a.openR, "打开(O)", a.press == "open", a.validSel())
	ui.Button(c, a.folderR, "打开所在文件夹(F)", a.press == "folder", a.validSel())
	ui.Button(c, a.closeR, "关闭", a.press == "close", true)

	status := ui.Rect{r.X, r.Bottom() - 20, r.W, 20}
	c.FillRect(status, ui.ColFace)
	c.HLine(status.X, status.Y, status.W, ui.ColShadow)
	c.HLine(status.X, status.Y+1, status.W, ui.ColWhite)
	msg := a.status
	if a.running {
		msg = fmt.Sprintf("正在查找… 已找到 %d 个项目", len(a.results))
	}
	f.TextClip(c, status.X+6, status.Y+3, status.W-12, msg, ui.ColText)
}

func (a *FindApp) drawField(c *ui.Canvas, f *ui.Font, r ui.Rect, value []rune, caret int, focused bool) {
	c.FillRect(r, ui.ColWhite)
	c.Sunken(r)
	f.TextClip(c, r.X+5, r.Y+4, r.W-10, string(value), ui.ColText)
	if focused {
		before := string(value[:caret])
		cx := r.X + 5 + f.Width(before)
		if cx < r.Right()-4 {
			c.VLine(cx, r.Y+3, 17, ui.ColText)
		}
	}
}

func (a *FindApp) findScrollRect() ui.Rect {
	inner := a.listR.Inset(2)
	scrollW := 16
	headH := 20
	return ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2}
}

func (a *FindApp) validSel() bool { return a.sel >= 0 && a.sel < len(a.results) }

func (a *FindApp) rowAt(x, y int) int {
	inner := a.listR.Inset(2)
	headH := 20
	if y < inner.Y+headH+1 {
		return -1
	}
	row := (y - inner.Y - headH - 1) / 21
	idx := a.scroll.Pos + row
	if idx >= 0 && idx < len(a.results) {
		return idx
	}
	return -1
}

func (a *FindApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	scroll := a.findScrollRect()
	if ev.Kind == MouseWheel {
		if a.listR.Contains(ev.X, ev.Y) {
			if changed, _ := a.scroll.Mouse(ev, scroll); changed {
				d.Invalidate()
			}
		}
		return
	}
	if ev.Kind == MouseMove || ev.Kind == MouseUp {
		if changed, _ := a.scroll.Mouse(ev, scroll); changed {
			d.Invalidate()
		}
		if ev.Kind == MouseMove {
			return
		}
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.listR.Contains(ev.X, ev.Y) && scroll.Contains(ev.X, ev.Y) {
			if changed, _ := a.scroll.Mouse(ev, scroll); changed {
				d.Invalidate()
			}
			return
		}
		switch {
		case a.nameR.Contains(ev.X, ev.Y):
			a.focus = 0
			a.queryCaret = runCaretAt(ui.DefaultFont(), a.query, a.nameR, ev.X)
			d.Invalidate()
		case a.scopeR.Contains(ev.X, ev.Y):
			a.focus = 1
			a.scopeCaret = runCaretAt(ui.DefaultFont(), a.scope, a.scopeR, ev.X)
			d.Invalidate()
		case a.recurseR.Contains(ev.X, ev.Y) || (ui.Rect{a.recurseR.X, a.recurseR.Y - 2, 120, 18}).Contains(ev.X, ev.Y):
			a.recursive = !a.recursive
			d.Invalidate()
		case a.hiddenR.Contains(ev.X, ev.Y) || (ui.Rect{a.hiddenR.X, a.hiddenR.Y - 2, 120, 18}).Contains(ev.X, ev.Y):
			a.hidden = !a.hidden
			d.Invalidate()
		case a.startR.Contains(ev.X, ev.Y):
			a.press = "start"
		case a.stopR.Contains(ev.X, ev.Y):
			a.press = "stop"
		case a.openR.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.folderR.Contains(ev.X, ev.Y):
			a.press = "folder"
		case a.closeR.Contains(ev.X, ev.Y):
			a.press = "close"
		case a.listR.Contains(ev.X, ev.Y):
			a.focus = 2
			idx := a.rowAt(ev.X, ev.Y)
			if idx >= 0 {
				a.sel = idx
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastIndex == idx {
					a.openSelected()
				}
				a.lastClick = time.Now()
				a.lastIndex = idx
			}
			d.Invalidate()
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "start":
			if a.startR.Contains(ev.X, ev.Y) {
				a.startSearch()
			}
		case "stop":
			if a.stopR.Contains(ev.X, ev.Y) {
				a.stopSearch()
			}
		case "open":
			if a.openR.Contains(ev.X, ev.Y) {
				a.openSelected()
			}
		case "folder":
			if a.folderR.Contains(ev.X, ev.Y) {
				a.openFolder()
			}
		case "close":
			if a.closeR.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
	}
}

func (a *FindApp) openSelected() {
	if !a.validSel() {
		return
	}
	e := a.results[a.sel]
	if e.isDir {
		a.d.openFiles(e.path)
		return
	}
	a.d.openDesktopPath(e.path)
}

func (a *FindApp) openFolder() {
	if !a.validSel() {
		return
	}
	e := a.results[a.sel]
	dir := e.path
	if !e.isDir {
		dir = e.dir
	} else {
		dir = filepath.Dir(e.path)
	}
	a.d.openFiles(dir)
}

func (a *FindApp) stopSearch() {
	if !a.running {
		return
	}
	a.stopped = true
	a.stop.Store(true)
	a.status = "正在停止查找…"
	a.d.Invalidate()
}

func (a *FindApp) startSearch() {
	a.token.Add(1)
	token := a.token.Load()
	a.stop.Store(false)
	a.stopped = false
	a.running = true
	a.done = false
	a.results = nil
	a.sel = -1
	a.scroll.Pos = 0
	a.status = "正在查找…"
	a.d.Invalidate()

	scope := expandRunPath(strings.TrimSpace(string(a.scope)))
	if scope == "" {
		scope = "/root"
	}
	st, err := os.Stat(scope)
	if err != nil || !st.IsDir() {
		a.running = false
		a.done = true
		a.status = "搜索范围无效：" + scope
		a.d.Invalidate()
		return
	}
	pattern := strings.ToLower(strings.TrimSpace(string(a.query)))
	if pattern == "" {
		pattern = "*"
	}
	if !strings.ContainsAny(pattern, "*?") {
		pattern = "*" + pattern + "*"
	}
	go a.walk(token, scope, pattern, a.recursive, a.hidden)
}

func skipFindDir(path string) bool {
	switch filepath.Clean(path) {
	case "/proc", "/sys", "/dev", "/run":
		return true
	}
	return false
}

func (a *FindApp) walk(token uint64, scope, pattern string, recursive, hidden bool) {
	var batch []findResult
	count := 0
	limited := false
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b := batch
		batch = nil
		a.d.Post(func() {
			if a.closed.Load() || a.token.Load() != token {
				return
			}
			a.results = append(a.results, b...)
			a.d.Invalidate()
		})
	}
	_ = filepath.WalkDir(scope, func(path string, de fs.DirEntry, err error) error {
		if a.closed.Load() || a.stop.Load() || a.token.Load() != token {
			return filepath.SkipAll
		}
		if err != nil {
			if de != nil && de.IsDir() && path != scope {
				return filepath.SkipDir
			}
			return nil
		}
		if path == scope {
			return nil
		}
		name := de.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			if de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if de.IsDir() {
			if !recursive || skipFindDir(path) {
				return filepath.SkipDir
			}
		}
		ok, _ := filepath.Match(pattern, strings.ToLower(name))
		if !ok {
			return nil
		}
		res := findResult{name: name, path: path, dir: filepath.Dir(path), isDir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			res.size = info.Size()
		}
		batch = append(batch, res)
		count++
		if count >= maxFindResults {
			limited = true
			flush()
			return filepath.SkipAll
		}
		if len(batch) >= 25 {
			flush()
		}
		return nil
	})
	flush()
	a.d.Post(func() {
		if a.closed.Load() || a.token.Load() != token {
			return
		}
		a.running = false
		a.done = true
		switch {
		case limited:
			a.status = fmt.Sprintf("已达到 %d 个结果上限，请缩小搜索范围。", maxFindResults)
		case a.stopped:
			a.status = fmt.Sprintf("查找已停止，共找到 %d 个项目。", len(a.results))
		default:
			a.status = fmt.Sprintf("找到 %d 个项目。", len(a.results))
		}
		a.d.Invalidate()
	})
}

func (a *FindApp) ensureSel() {
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

func (a *FindApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.focus < 2 {
		value := &a.query
		caret := &a.queryCaret
		if a.focus == 1 {
			value = &a.scope
			caret = &a.scopeCaret
		}
		switch ev.Name {
		case "Esc":
			a.stopSearch()
			d.CloseWindow(w)
			return true
		case "Tab":
			a.focus = (a.focus + 1) % 3
			return true
		case "Enter":
			a.startSearch()
			return true
		case "Backspace":
			if *caret > 0 {
				*value = append((*value)[:*caret-1], (*value)[*caret:]...)
				*caret--
			}
			return true
		case "Delete":
			if *caret < len(*value) {
				*value = append((*value)[:*caret], (*value)[*caret+1:]...)
			}
			return true
		case "Left":
			if *caret > 0 {
				*caret--
			}
			return true
		case "Right":
			if *caret < len(*value) {
				*caret++
			}
			return true
		case "Home":
			*caret = 0
			return true
		case "End":
			*caret = len(*value)
			return true
		}
		if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
			*value = append(*value, 0)
			copy((*value)[*caret+1:], (*value)[*caret:])
			(*value)[*caret] = ev.Rune
			*caret++
			return true
		}
		return false
	}

	switch ev.Name {
	case "Esc":
		a.stopSearch()
		d.CloseWindow(w)
		return true
	case "Tab":
		a.focus = 0
		return true
	case "Enter":
		a.openSelected()
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
			a.ensureSel()
		}
		return true
	case "Down":
		if a.sel < len(a.results)-1 {
			a.sel++
			a.ensureSel()
		}
		return true
	case "PgUp":
		a.sel -= a.scroll.Visible
		if a.sel < 0 {
			a.sel = 0
		}
		a.ensureSel()
		return true
	case "PgDn":
		a.sel += a.scroll.Visible
		if a.sel >= len(a.results) {
			a.sel = len(a.results) - 1
		}
		a.ensureSel()
		return true
	case "F5":
		a.startSearch()
		return true
	}
	return false
}

func (a *FindApp) Close() {
	a.closed.Store(true)
	a.stop.Store(true)
	a.token.Add(1)
}
