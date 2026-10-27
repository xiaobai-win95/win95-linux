package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"win95linux/internal/ui"
)

type FileMode int

const (
	FileOpen FileMode = iota
	FileSave
)

type fileEntry struct {
	name  string
	isDir bool
	size  int64
	mod   time.Time
}

type FileDialogApp struct {
	Base
	d        *Desktop
	mode     FileMode
	dir      string
	filename []rune
	caret    int
	entries  []fileEntry
	sel      int
	scroll   VScroll
	err      string
	focus    int // 0 list, 1 name
	win      *Window
	cb       func(path string, ok bool)

	upR, okR, cancelR, nameR, listR ui.Rect
	press                           string
	lastClick                       time.Time
	lastIndex                       int
}

func (d *Desktop) FileDialog(mode FileMode, dir, name string, cb func(string, bool)) *Window {
	if dir == "" {
		dir = "/root/文档"
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = "/"
	}
	a := &FileDialogApp{d: d, mode: mode, dir: dir, cb: cb, sel: -1, lastIndex: -1}
	a.filename = []rune(name)
	a.caret = len(a.filename)
	if mode == FileSave {
		a.focus = 1
	}
	a.load()
	w := d.OpenModal(a)
	w.W = 560
	w.H = 430
	w.MinW, w.MinH = w.W, w.H
	w.X = (d.W - w.W) / 2
	w.Y = (d.taskbarTop() - w.H) / 2
	a.win = w
	return w
}

func (a *FileDialogApp) Title() string {
	if a.mode == FileSave {
		return "另存为"
	}
	return "打开"
}

func (a *FileDialogApp) Icon() ui.IconKind       { return ui.IcoFolder }
func (a *FileDialogApp) DefaultSize() (int, int) { return 560, 430 }
func (a *FileDialogApp) MinSize() (int, int)     { return 560, 430 }
func (a *FileDialogApp) Resizable() bool         { return false }

func (a *FileDialogApp) load() {
	a.entries = nil
	ents, err := os.ReadDir(a.dir)
	if err != nil {
		a.err = "无法读取文件夹: " + err.Error()
		return
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		a.entries = append(a.entries, fileEntry{name: name, isDir: e.IsDir(), size: info.Size(), mod: info.ModTime()})
	}
	sort.SliceStable(a.entries, func(i, j int) bool {
		if a.entries[i].isDir != a.entries[j].isDir {
			return a.entries[i].isDir
		}
		return strings.ToLower(a.entries[i].name) < strings.ToLower(a.entries[j].name)
	})
	a.sel = -1
	a.scroll.Pos = 0
	a.err = ""
}

func (a *FileDialogApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	// Location row.
	f.Text(c, r.X+8, r.Y+9, "位置:", ui.ColText)
	pathR := ui.Rect{r.X + 46, r.Y + 6, r.W - 46 - 90, 24}
	c.FillRect(pathR, ui.ColWhite)
	c.Sunken(pathR)
	f.TextClip(c, pathR.X+5, pathR.Y+4, pathR.W-10, a.dir, ui.ColText)
	a.upR = ui.Rect{r.Right() - 78, r.Y + 6, 70, 24}
	ui.Button(c, a.upR, "上一级", a.press == "up", a.dir != "/")

	// File list.
	a.listR = ui.Rect{r.X + 8, r.Y + 38, r.W - 16, r.Bottom() - r.Y - 38 - 72}
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	scrollW := 16
	nameW := inner.W - scrollW - 4 - 150
	if nameW < 180 {
		nameW = 180
	}
	headH := 20
	c.FillRect(ui.Rect{inner.X, inner.Y, inner.W - scrollW, headH}, ui.ColFace)
	c.HLine(inner.X, inner.Y+headH-1, inner.W-scrollW, ui.ColShadow)
	f.Text(c, inner.X+5, inner.Y+3, "名称", ui.ColText)
	f.Text(c, inner.X+nameW, inner.Y+3, "大小", ui.ColText)
	f.Text(c, inner.X+nameW+90, inner.Y+3, "修改时间", ui.ColText)
	rows := (inner.H - headH - 4) / 20
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.entries)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	y := inner.Y + headH + 1
	for i := a.scroll.Pos; i < len(a.entries) && y+20 <= inner.Bottom(); i++ {
		e := a.entries[i]
		rr := ui.Rect{inner.X + 1, y, inner.W - scrollW - 2, 19}
		if i == a.sel {
			c.FillRect(rr, ui.ColNavy)
		}
		kind := ui.IcoFile
		if e.isDir {
			kind = ui.IcoFolder
		}
		ui.DrawIcon16(c, kind, rr.X+3, rr.Y+1)
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		f.TextClip(c, rr.X+23, rr.Y+2, nameW-24, e.name, col)
		if e.isDir {
			f.Text(c, inner.X+nameW, rr.Y+2, "<文件夹>", col)
		} else {
			f.Text(c, inner.X+nameW, rr.Y+2, sizeText(e.size), col)
			f.Text(c, inner.X+nameW+90, rr.Y+2, e.mod.Format("2006-01-02 15:04"), col)
		}
		y += 20
	}
	sr := ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2}
	a.scroll.Draw(c, sr)

	// Filename row.
	fy := r.Bottom() - 64
	f.Text(c, r.X+8, fy+7, "文件名:", ui.ColText)
	a.nameR = ui.Rect{r.X + 54, fy + 3, r.W - 54 - 16, 25}
	c.FillRect(a.nameR, ui.ColWhite)
	c.Sunken(a.nameR)
	txt := string(a.filename)
	if f.Width(txt) > a.nameR.W-10 {
		txt = "…" + string([]rune(txt)[maxInt(0, len([]rune(txt))-20):])
	}
	f.Text(c, a.nameR.X+5, a.nameR.Y+4, txt, ui.ColText)
	if a.focus == 1 {
		before := string(a.filename[:a.caret])
		cx := a.nameR.X + 5 + f.Width(before)
		if cx < a.nameR.Right()-4 {
			c.VLine(cx, a.nameR.Y+3, 17, ui.ColText)
		}
	}
	a.okR = ui.Rect{r.Right() - 188, fy + 36, 84, 26}
	a.cancelR = ui.Rect{r.Right() - 96, fy + 36, 84, 26}
	okLabel := "打开"
	if a.mode == FileSave {
		okLabel = "保存"
	}
	ui.Button(c, a.okR, okLabel, a.press == "ok", true)
	ui.Button(c, a.cancelR, "取消", a.press == "cancel", true)
	if a.err != "" {
		f.TextClip(c, r.X+8, r.Bottom()-22, r.W-16, a.err, ui.ColRed)
	}
}

func sizeText(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d 字节", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *FileDialogApp) listRects() (ui.Rect, ui.Rect) {
	inner := a.listR.Inset(2)
	scrollW := 16
	headH := 20
	return ui.Rect{inner.X, inner.Y + headH + 1, inner.W - scrollW - 2, inner.H - headH - 2},
		ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2}
}

func (a *FileDialogApp) rowAt(x, y int) int {
	inner := a.listR.Inset(2)
	headH := 20
	if y < inner.Y+headH+1 {
		return -1
	}
	row := (y - inner.Y - headH - 1) / 20
	idx := a.scroll.Pos + row
	if idx >= 0 && idx < len(a.entries) {
		return idx
	}
	return -1
}

func (a *FileDialogApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	scrollRect := func() ui.Rect { _, s := a.listRects(); return s }()
	if changed, _ := a.scroll.Mouse(ev, scrollRect); changed {
		d.Invalidate()
	}
	if ev.Kind == MouseWheel {
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.upR.Contains(ev.X, ev.Y):
			a.press = "up"
		case a.okR.Contains(ev.X, ev.Y):
			a.press = "ok"
		case a.cancelR.Contains(ev.X, ev.Y):
			a.press = "cancel"
		case a.nameR.Contains(ev.X, ev.Y):
			a.focus = 1
		case a.listR.Contains(ev.X, ev.Y):
			a.focus = 0
			idx := a.rowAt(ev.X, ev.Y)
			if idx >= 0 {
				a.sel = idx
				a.filename = []rune(a.entries[idx].name)
				a.caret = len(a.filename)
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastIndex == idx {
					a.activate(idx)
				}
				a.lastClick = time.Now()
				a.lastIndex = idx
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "up":
			if a.upR.Contains(ev.X, ev.Y) {
				a.navigateUp()
			}
		case "ok":
			if a.okR.Contains(ev.X, ev.Y) {
				a.accept()
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) {
				a.finish("", false)
			}
		}
	}
}

func (a *FileDialogApp) navigateUp() {
	if a.dir == "/" {
		return
	}
	a.dir = filepath.Dir(a.dir)
	a.load()
}

func (a *FileDialogApp) activate(idx int) {
	if idx < 0 || idx >= len(a.entries) {
		return
	}
	e := a.entries[idx]
	if e.isDir {
		a.dir = filepath.Join(a.dir, e.name)
		a.load()
		return
	}
	a.filename = []rune(e.name)
	a.caret = len(a.filename)
	a.accept()
}

func (a *FileDialogApp) accept() {
	name := strings.TrimSpace(string(a.filename))
	if name == "" {
		a.err = "请输入文件名。"
		return
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.dir, name)
	}
	if a.mode == FileOpen {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			a.err = "找不到文件: " + name
			return
		}
		a.finish(path, true)
		return
	}
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		dlg := a
		a.d.MessageBox("另存为", "文件“"+st.Name()+"”已经存在，要替换它吗？", IconQuestion,
			[]string{"是", "否"}, func(i int) {
				if i == 0 {
					dlg.finish(path, true)
				}
			})
		return
	}
	a.finish(path, true)
}

func (a *FileDialogApp) finish(path string, ok bool) {
	win := a.win
	cb := a.cb
	if win != nil {
		a.d.CloseWindowForced(win)
	}
	if cb != nil {
		cb(path, ok)
	}
}

func (a *FileDialogApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		a.finish("", false)
		return true
	case "Enter":
		a.accept()
		return true
	case "Tab":
		a.focus = 1 - a.focus
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
			a.ensureSel()
		}
		return true
	case "Down":
		if a.sel < len(a.entries)-1 {
			a.sel++
			a.ensureSel()
		}
		return true
	case "PgUp":
		a.sel -= 8
		if a.sel < 0 {
			a.sel = 0
		}
		a.ensureSel()
		return true
	case "PgDn":
		a.sel += 8
		if a.sel >= len(a.entries) {
			a.sel = len(a.entries) - 1
		}
		a.ensureSel()
		return true
	case "Backspace":
		if a.caret > 0 {
			a.filename = append(a.filename[:a.caret-1], a.filename[a.caret:]...)
			a.caret--
		}
		return true
	case "Delete":
		if a.caret < len(a.filename) {
			a.filename = append(a.filename[:a.caret], a.filename[a.caret+1:]...)
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
		}
		return true
	case "Right":
		if a.caret < len(a.filename) {
			a.caret++
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(a.filename)
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.focus = 1
		a.filename = append(a.filename, 0)
		copy(a.filename[a.caret+1:], a.filename[a.caret:])
		a.filename[a.caret] = ev.Rune
		a.caret++
		return true
	}
	return false
}

func (a *FileDialogApp) ensureSel() {
	if a.sel < 0 {
		return
	}
	a.scroll.Visible = maxInt(1, a.scroll.Visible)
	if a.sel < a.scroll.Pos {
		a.scroll.Pos = a.sel
	}
	if a.sel >= a.scroll.Pos+a.scroll.Visible {
		a.scroll.Pos = a.sel - a.scroll.Visible + 1
	}
	if a.sel < len(a.entries) {
		a.filename = []rune(a.entries[a.sel].name)
		a.caret = len(a.filename)
	}
}

func (a *FileDialogApp) Close() {}
