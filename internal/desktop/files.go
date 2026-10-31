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

type fmEntry struct {
	name  string
	path  string
	isDir bool
	kind  ui.IconKind
	size  int64
	mod   time.Time
	typ   string
}

type FileManagerApp struct {
	Base
	d         *Desktop
	win       *Window
	path      string
	entries   []fmEntry
	sel       int
	scroll    VScroll
	press     string
	loadErr   string
	lastClick time.Time
	lastSel   int

	down     bool
	dragPath string
	dragName string
	dragX    int
	dragY    int

	btnUp, btnNew, btnDelete ui.Rect
	listR, statusR           ui.Rect
}

func NewFileManagerApp(d *Desktop, path string) *FileManagerApp {
	a := &FileManagerApp{d: d, path: path, sel: -1, lastSel: -1}
	a.load()
	return a
}

func (a *FileManagerApp) Title() string {
	if a.path == "" {
		return "我的电脑"
	}
	if a.path == "/" {
		return "系统盘 (C:)"
	}
	if a.path == trashDir {
		return "回收站"
	}
	return filepath.Base(a.path) + " - 文件管理器"
}

func (a *FileManagerApp) Icon() ui.IconKind {
	if a.path == "" {
		return ui.IcoComputer
	}
	return ui.IcoFolder
}

func (a *FileManagerApp) DefaultSize() (int, int) { return 760, 520 }
func (a *FileManagerApp) MinSize() (int, int)     { return 480, 320 }
func (a *FileManagerApp) Resizable() bool         { return true }

func (a *FileManagerApp) Menus(d *Desktop, w *Window) []Menu {
	fileItems := []MenuItem{
		{Label: "新建文件夹(N)", Enabled: a.path != "" && a.path != trashDir, Action: func() { a.newFolder(d) }},
		{Label: "删除(D)", Accel: "Del", Enabled: a.sel >= 0, Action: func() { a.deleteSelected(d) }},
	}
	if a.path == trashDir {
		fileItems = append(fileItems, MenuItem{Label: "清空回收站(E)", Enabled: d.recycleCount() > 0, Action: func() { d.emptyRecycle(a.load) }})
	}
	fileItems = append(fileItems,
		MenuItem{Separator: true},
		MenuItem{Label: "打开(O)", Enabled: a.sel >= 0, Action: func() { a.activate(a.sel) }},
		MenuItem{Label: "向上(U)", Accel: "Backspace", Enabled: a.path != "", Action: func() { a.up() }},
		MenuItem{Separator: true},
		MenuItem{Label: "关闭(C)", Action: func() { d.CloseWindow(w) }},
	)
	return []Menu{
		{Label: "文件(F)", Items: fileItems},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "刷新(R)", Accel: "F5", Action: func() { a.load() }},
			{Separator: true},
			{Label: "详细资料(D)", Checked: true, Enabled: true},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于文件管理器(A)", Action: func() {
				d.MessageBox("关于文件管理器", "Windows 95 Linux 文件管理器\n版本 1.0\n\n浏览磁盘文件，打开文档与网页，删除的文件会放入回收站。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *FileManagerApp) load() {
	a.entries = nil
	a.sel = -1
	a.loadErr = ""
	if a.path == "" {
		a.entries = []fmEntry{
			{name: "系统盘 (C:)", path: "/", isDir: true, kind: ui.IcoDrive, typ: "本地磁盘"},
			{name: "我的文档", path: "/root/文档", isDir: true, kind: ui.IcoFolder, typ: "文件夹"},
			{name: "下载", path: "/root/下载", isDir: true, kind: ui.IcoDownload, typ: "文件夹"},
			{name: "终端", path: "@terminal", kind: ui.IcoTerminal, typ: "程序"},
			{name: "网络浏览器", path: "@browser", kind: ui.IcoBrowser, typ: "程序"},
		}
		return
	}
	ents, err := os.ReadDir(a.path)
	if err != nil {
		a.loadErr = err.Error()
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
		kind := ui.IcoFile
		typ := "文件"
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(a.path, name)); err == nil {
				isDir = st.IsDir()
			}
		}
		if isDir {
			kind = ui.IcoFolder
			typ = "文件夹"
		} else {
			switch strings.ToLower(filepath.Ext(name)) {
			case ".txt", ".md", ".log", ".conf", ".ini", ".cfg":
				kind = ui.IcoText
				typ = "文本文档"
			case ".html", ".htm":
				kind = ui.IcoBrowser
				typ = "HTML 文档"
			}
		}
		a.entries = append(a.entries, fmEntry{
			name: name, path: filepath.Join(a.path, name), isDir: isDir,
			kind: kind, size: info.Size(), mod: info.ModTime(), typ: typ,
		})
	}
	sort.SliceStable(a.entries, func(i, j int) bool {
		if a.entries[i].isDir != a.entries[j].isDir {
			return a.entries[i].isDir
		}
		return strings.ToLower(a.entries[i].name) < strings.ToLower(a.entries[j].name)
	})
	a.scroll.Pos = 0
}

func (a *FileManagerApp) up() {
	if a.path == "" {
		return
	}
	if a.path == "/" {
		a.path = ""
	} else {
		a.path = filepath.Dir(a.path)
	}
	a.load()
}

func (a *FileManagerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	tx := r.X + 4
	ty := r.Y + 4
	a.btnUp = ui.Rect{tx, ty, 68, 24}
	a.btnNew = ui.Rect{tx + 72, ty, 78, 24}
	a.btnDelete = ui.Rect{tx + 154, ty, 58, 24}
	ui.Button(c, a.btnUp, "上一级", a.press == "up", a.path != "")
	ui.Button(c, a.btnNew, "新建文件夹", a.press == "new", a.path != "" && a.path != trashDir)
	ui.Button(c, a.btnDelete, "删除", a.press == "del", a.sel >= 0)
	c.HLine(r.X+4, r.Y+31, r.W-8, ui.ColShadow)
	c.HLine(r.X+4, r.Y+32, r.W-8, ui.ColWhite)

	addr := ui.Rect{r.X + 6, r.Y + 36, r.W - 12, 24}
	c.FillRect(addr, ui.ColWhite)
	c.Sunken(addr)
	ui.DrawIcon16(c, ui.IcoFolder, addr.X+4, addr.Y+4)
	loc := a.path
	if loc == "" {
		loc = "我的电脑"
	}
	f.TextClip(c, addr.X+24, addr.Y+4, addr.W-30, loc, ui.ColText)

	a.listR = ui.Rect{r.X + 6, addr.Bottom() + 6, r.W - 12, r.H - (addr.Bottom() + 6 - r.Y) - 28}
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	headH := 20
	scrollW := 16
	colW := []int{inner.W - scrollW - 2 - 100 - 90 - 150, 100, 90, 150}
	if colW[0] < 160 {
		colW[0] = 160
	}
	c.FillRect(ui.Rect{inner.X, inner.Y, inner.W - scrollW, headH}, ui.ColFace)
	heads := []string{"名称", "大小", "类型", "修改时间"}
	hx := inner.X + 4
	for i, h := range heads {
		f.Text(c, hx, inner.Y+3, h, ui.ColText)
		hx += colW[i]
	}
	if len(a.entries) == 0 {
		msg := "此文件夹为空。"
		if a.loadErr != "" {
			msg = "无法读取文件夹: " + a.loadErr
		}
		f.Text(c, inner.X+10, inner.Y+headH+8, msg, ui.ColShadow)
	}
	rows := (inner.H - headH - 4) / 21
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.entries)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	y := inner.Y + headH + 1
	for i := a.scroll.Pos; i < len(a.entries) && y+20 <= inner.Bottom(); i++ {
		e := a.entries[i]
		rr := ui.Rect{inner.X + 1, y, inner.W - scrollW - 2, 20}
		if i == a.sel {
			c.FillRect(rr, ui.ColNavy)
		}
		ui.DrawIcon16(c, e.kind, rr.X+3, rr.Y+2)
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		f.TextClip(c, rr.X+23, rr.Y+2, colW[0]-26, e.name, col)
		if !e.isDir {
			f.Text(c, inner.X+colW[0], rr.Y+2, sizeText(e.size), col)
		} else {
			f.Text(c, inner.X+colW[0], rr.Y+2, "", col)
		}
		f.Text(c, inner.X+colW[0]+colW[1], rr.Y+2, e.typ, col)
		if e.path != "" && e.path[0] != '@' {
			f.Text(c, inner.X+colW[0]+colW[1]+colW[2], rr.Y+2, e.mod.Format("2006-01-02 15:04"), col)
		}
		y += 21
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2})

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	info := fmt.Sprintf("%d 个对象", len(a.entries))
	if a.sel >= 0 && a.sel < len(a.entries) {
		info = a.entries[a.sel].name + "　·　" + info
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, info, ui.ColText)
}

func (a *FileManagerApp) listGeometry() (ui.Rect, ui.Rect, int) {
	inner := a.listR.Inset(2)
	scrollW := 16
	headH := 20
	rows := (inner.H - headH - 4) / 21
	if rows < 1 {
		rows = 1
	}
	return ui.Rect{inner.X, inner.Y + headH + 1, inner.W - scrollW - 2, inner.H - headH - 2},
		ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2}, rows
}

func (a *FileManagerApp) rowAt(x, y int) int {
	inner := a.listR.Inset(2)
	headH := 20
	if y < inner.Y+headH+1 {
		return -1
	}
	row := (y - inner.Y - headH - 1) / 21
	idx := a.scroll.Pos + row
	if idx >= 0 && idx < len(a.entries) {
		return idx
	}
	return -1
}

func (a *FileManagerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	_, sr, _ := a.listGeometry()
	if changed, consumed := a.scroll.Mouse(ev, sr); changed {
		d.Invalidate()
		if consumed && ev.Kind != MouseUp {
			return
		}
	}
	if ev.Kind == MouseWheel {
		return
	}
	if ev.Kind == MouseMove {
		if a.down && a.dragPath != "" && d.fileDrag == nil {
			if absInt(ev.X-a.dragX)+absInt(ev.Y-a.dragY) > 6 {
				d.fileDrag = &fileDrag{name: a.dragName, path: a.dragPath}
				d.Invalidate()
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
		a.down = true
		a.dragPath, a.dragName = "", ""
		switch {
		case a.btnUp.Contains(ev.X, ev.Y):
			a.press = "up"
		case a.btnNew.Contains(ev.X, ev.Y):
			a.press = "new"
		case a.btnDelete.Contains(ev.X, ev.Y):
			a.press = "del"
		case a.listR.Contains(ev.X, ev.Y):
			idx := a.rowAt(ev.X, ev.Y)
			if idx >= 0 {
				a.sel = idx
				e := a.entries[idx]
				if e.path != "" && !strings.HasPrefix(e.path, "@") {
					a.dragPath, a.dragName = e.path, e.name
					a.dragX, a.dragY = ev.X, ev.Y
				}
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastSel == idx {
					a.dragPath = ""
					a.activate(idx)
				}
				a.lastClick = time.Now()
				a.lastSel = idx
			}
		}
	case MouseUp:
		a.down = false
		p := a.press
		a.press = ""
		if d.fileDrag != nil {
			a.dragPath = ""
			target := d.fileDragTarget(ev.X, ev.Y)
			d.dropFileDrag(ev.X, ev.Y)
			if target == dropRecycle {
				a.load()
			}
			return
		}
		a.dragPath = ""
		switch p {
		case "up":
			if a.btnUp.Contains(ev.X, ev.Y) {
				a.up()
			}
		case "new":
			if a.btnNew.Contains(ev.X, ev.Y) {
				a.newFolder(d)
			}
		case "del":
			if a.btnDelete.Contains(ev.X, ev.Y) {
				a.deleteSelected(d)
			}
		}
	}
}

func (a *FileManagerApp) activate(idx int) {
	if idx < 0 || idx >= len(a.entries) {
		return
	}
	e := a.entries[idx]
	switch e.path {
	case "@terminal":
		a.d.openTerminal()
		return
	case "@browser":
		a.d.openBrowser("")
		return
	}
	if e.isDir {
		a.path = e.path
		a.load()
		return
	}
	switch strings.ToLower(filepath.Ext(e.name)) {
	case ".txt", ".md", ".log", ".conf", ".ini", ".cfg":
		a.d.openNotepad(e.path)
	case ".html", ".htm":
		a.d.openBrowser("file://" + e.path)
	default:
		a.d.MessageBox("文件管理器", "无法打开“"+e.name+"”。\n\n没有与此文件类型关联的程序。", IconInfo, []string{"确定"}, nil)
	}
}

func protectedPath(p string) bool {
	switch filepath.Clean(p) {
	case "/", "/bin", "/sbin", "/etc", "/usr", "/lib", "/var", "/boot", "/proc", "/sys", "/dev", "/root", "/home", "/tmp":
		return true
	}
	return false
}

func (a *FileManagerApp) newFolder(d *Desktop) {
	if a.path == "" || a.path == trashDir {
		return
	}
	base := "新建文件夹"
	name := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(a.path, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	if err := os.Mkdir(filepath.Join(a.path, name), 0o755); err != nil {
		d.MessageBox("文件管理器", "无法新建文件夹：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a.load()
	for i, e := range a.entries {
		if e.name == name {
			a.sel = i
		}
	}
}

func (a *FileManagerApp) deleteSelected(d *Desktop) {
	if a.sel < 0 || a.sel >= len(a.entries) {
		return
	}
	e := a.entries[a.sel]
	if protectedPath(e.path) || e.path == "" {
		d.MessageBox("文件管理器", "不能删除系统文件夹“"+e.name+"”。", IconWarning, []string{"确定"}, nil)
		return
	}
	if a.path == trashDir {
		d.MessageBox("确认删除", "确实要永久删除“"+e.name+"”吗？\n\n此操作无法撤销。", IconQuestion,
			[]string{"是", "否"}, func(i int) {
				if i != 0 {
					return
				}
				if err := os.RemoveAll(e.path); err != nil {
					d.MessageBox("文件管理器", "删除失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
					return
				}
				a.load()
				d.Invalidate()
			})
		return
	}
	d.MessageBox("确认删除", "确实要把“"+e.name+"”放入回收站吗？", IconQuestion,
		[]string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			trash := trashDir
			if err := os.MkdirAll(trash, 0o755); err != nil {
				d.MessageBox("文件管理器", "无法创建回收站：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			dest := filepath.Join(trash, e.name)
			if _, err := os.Stat(dest); err == nil {
				dest = filepath.Join(trash, fmt.Sprintf("%s-%d", e.name, time.Now().Unix()))
			}
			if err := os.Rename(e.path, dest); err != nil {
				d.MessageBox("文件管理器", "删除失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.load()
			d.Invalidate()
		})
}

func (a *FileManagerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Enter":
		a.activate(a.sel)
		return true
	case "Backspace":
		a.up()
		return true
	case "Delete":
		a.deleteSelected(d)
		return true
	case "Esc":
		if d.fileDrag != nil {
			d.fileDrag = nil
			a.dragPath = ""
			d.Invalidate()
			return true
		}
		return false
	case "F5":
		a.load()
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
	}
	if ev.Ctrl && ev.Name == "N" {
		a.newFolder(d)
		return true
	}
	return false
}

func (a *FileManagerApp) ensureSel() {
	if a.sel < 0 {
		return
	}
	_, _, rows := a.listGeometry()
	if a.sel < a.scroll.Pos {
		a.scroll.Pos = a.sel
	}
	if a.sel >= a.scroll.Pos+rows {
		a.scroll.Pos = a.sel - rows + 1
	}
}

func (a *FileManagerApp) Close() {}
