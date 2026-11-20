package desktop

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const archRootDir = "/opt/archlinux"

type fmEntry struct {
	name    string
	path    string
	isDir   bool
	kind    ui.IconKind
	size    int64
	mod     time.Time
	typ     string
	virtual bool
}

type fmTabState struct {
	path   string
	sel    int
	scroll int
}

type fmShare struct {
	source string
	mount  string
	fstype string
}

func (s fmShare) label() string {
	base := filepath.Base(s.mount)
	if base == "" || base == "." || base == "/" {
		base = filepath.Base(s.source)
	}
	if base == "" || base == "." || base == "/" {
		base = s.mount
	}
	return "共享文件夹 - " + base
}

type FileManagerApp struct {
	Base
	d   *Desktop
	win *Window

	tabs []fmTabState
	tab  int

	path       string
	entries    []fmEntry
	sel        int
	scroll     VScroll
	showHidden bool

	press     string
	loadErr   string
	lastClick time.Time
	lastSel   int

	down     bool
	dragPath string
	dragName string
	dragX    int
	dragY    int

	btnUp, btnNew, btnDelete, btnTerm, btnHidden ui.Rect
	tabBarR                                      ui.Rect
	tabRects, tabCloseRects                      []ui.Rect
	tabPlusR                                     ui.Rect
	listR, statusR                               ui.Rect
}

func NewFileManagerApp(d *Desktop, path string) *FileManagerApp {
	a := &FileManagerApp{
		d:       d,
		path:    path,
		sel:     -1,
		lastSel: -1,
		tabs:    []fmTabState{{path: path, sel: -1}},
	}
	a.load()
	return a
}

func fmPathLabel(path string) string {
	switch path {
	case "":
		return "我的电脑"
	case "/":
		return "系统盘 (C:)"
	case trashDir:
		return "回收站"
	case archRootDir:
		return "Arch Linux 子系统"
	}
	return filepath.Base(path)
}

func (a *FileManagerApp) Title() string {
	switch a.path {
	case "", "/", trashDir, archRootDir:
		return fmPathLabel(a.path)
	}
	return fmPathLabel(a.path) + " - 文件管理器"
}

func (a *FileManagerApp) Icon() ui.IconKind {
	switch a.path {
	case "":
		return ui.IcoComputer
	case trashDir:
		return ui.IcoRecycle
	case archRootDir:
		return ui.IcoDrive
	}
	return ui.IcoFolder
}

func (a *FileManagerApp) DefaultSize() (int, int) { return 860, 560 }
func (a *FileManagerApp) MinSize() (int, int)     { return 520, 360 }
func (a *FileManagerApp) Resizable() bool         { return true }

func (a *FileManagerApp) selectedReal() (fmEntry, bool) {
	if a.sel < 0 || a.sel >= len(a.entries) {
		return fmEntry{}, false
	}
	e := a.entries[a.sel]
	if e.virtual || e.path == "" || strings.HasPrefix(e.path, "@") {
		return fmEntry{}, false
	}
	return e, true
}

func (a *FileManagerApp) inArch() bool {
	return a.path == archRootDir || strings.HasPrefix(a.path, archRootDir+"/")
}

func (a *FileManagerApp) terminalLabel() string {
	if a.inArch() {
		return "在此处打开 Arch 终端"
	}
	return "在此处打开终端"
}

func (a *FileManagerApp) Menus(d *Desktop, w *Window) []Menu {
	e, hasReal := a.selectedReal()
	renamable := hasReal && !protectedPath(e.path)
	inTrash := a.path == trashDir
	fileItems := []MenuItem{
		{Label: "打开(O)", Enabled: a.sel >= 0, Action: func() { a.activate(a.sel) }},
		{Label: a.terminalLabel(), Action: func() { a.openTerminalHere(d) }},
		{Label: "创建快捷方式(S)", Enabled: hasReal && !inTrash, Action: func() { a.createShortcut(d) }},
		{Separator: true},
		{Label: "新建文件夹(N)", Accel: "Ctrl+N", Enabled: a.path != "" && !inTrash, Action: func() { a.newFolder(d) }},
		{Label: "新建标签页(T)", Accel: "Ctrl+T", Action: func() { a.newTab(); d.Invalidate() }},
		{Label: "关闭标签页(W)", Accel: "Ctrl+W", Enabled: len(a.tabs) > 1, Action: func() { a.closeTab(); d.Invalidate() }},
		{Separator: true},
		{Label: "重命名(M)", Accel: "F2", Enabled: renamable, Action: func() { a.renameSelected(d) }},
		{Label: "删除(D)", Accel: "Del", Enabled: hasReal && !protectedPath(e.path), Action: func() { a.deleteSelected(d) }},
	}
	if inTrash {
		fileItems = append(fileItems, MenuItem{Label: "清空回收站(E)", Enabled: d.recycleCount() > 0, Action: func() { d.emptyRecycle(a.load) }})
	}
	fileItems = append(fileItems,
		MenuItem{Separator: true},
		MenuItem{Label: "向上(U)", Accel: "Backspace", Enabled: a.path != "", Action: func() { a.up(); d.Invalidate() }},
		MenuItem{Label: "关闭(C)", Action: func() { d.CloseWindow(w) }},
	)

	goItems := []MenuItem{
		{Label: "我的电脑(C)", Action: func() { a.navigate(d, "") }},
		{Label: "桌面(D)", Action: func() { a.navigate(d, "/root/桌面") }},
		{Label: "我的文档(M)", Action: func() { a.navigate(d, "/root/文档") }},
		{Label: "回收站(R)", Action: func() { a.navigate(d, trashDir) }},
	}
	if fmArchInstalled() {
		goItems = append(goItems, MenuItem{Label: "Arch Linux 子系统(A)", Action: func() { a.navigate(d, archRootDir) }})
	}
	for _, sh := range fmSharedMounts() {
		sh := sh
		goItems = append(goItems, MenuItem{Label: sh.label(), Action: func() { a.navigate(d, sh.mount) }})
	}

	return []Menu{
		{Label: "文件(F)", Items: fileItems},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "刷新(R)", Accel: "F5", Action: func() { a.reload(true); d.Invalidate() }},
			{Separator: true},
			{Label: "显示隐藏文件(H)", Accel: "Ctrl+H", Checked: a.showHidden, Enabled: true, Action: func() { a.toggleHidden(d) }},
			{Separator: true},
			{Label: "详细资料(D)", Checked: true, Enabled: true},
		}},
		{Label: "转到(G)", Items: goItems},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于文件管理器(A)", Action: func() {
				d.MessageBox("关于文件管理器", "Windows 95 Linux 文件管理器\n版本 2.0\n\n"+
					"· 标签页：Ctrl+T 新建，Ctrl+W 关闭，Ctrl+Tab 切换\n"+
					"· 在此处打开终端（Arch 子系统内会进入 chroot 终端）\n"+
					"· 创建快捷方式：为选中项目建立符号链接\n"+
					"· 显示隐藏文件：Ctrl+H\n"+
					"· 自动识别 UTM / virtiofs / 9p 共享文件夹\n"+
					"· 删除的文件会放入 "+trashDir+" 回收站", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *FileManagerApp) navigate(d *Desktop, path string) {
	a.path = path
	a.load()
	d.Invalidate()
}

func (a *FileManagerApp) load() { a.reload(false) }

func (a *FileManagerApp) reload(keep bool) {
	psel, pscroll := a.sel, a.scroll.Pos
	a.entries = nil
	a.loadErr = ""
	if a.path == "" {
		a.entries = append(a.entries, fmEntry{name: "系统盘 (C:)", path: "/", isDir: true, kind: ui.IcoDrive, typ: "本地磁盘", virtual: true})
		for _, item := range []struct{ label, path string }{
			{"我的文档", "/root/文档"},
			{"下载", "/root/下载"},
			{"桌面", "/root/桌面"},
		} {
			if st, err := os.Stat(item.path); err == nil && st.IsDir() {
				a.entries = append(a.entries, fmEntry{name: item.label, path: item.path, isDir: true, kind: ui.IcoFolder, typ: "文件夹", virtual: true})
			}
		}
		if fmArchInstalled() {
			a.entries = append(a.entries,
				fmEntry{name: "Arch Linux 子系统", path: archRootDir, isDir: true, kind: ui.IcoDrive, typ: "glibc 子系统", virtual: true},
				fmEntry{name: "Arch 子系统终端", path: "@archterm", kind: ui.IcoTerminal, typ: "程序", virtual: true},
			)
		}
		for _, sh := range fmSharedMounts() {
			a.entries = append(a.entries, fmEntry{
				name: sh.label(), path: sh.mount, isDir: true, kind: ui.IcoDrive,
				typ: "共享文件夹 (" + sh.fstype + ")", virtual: true,
			})
		}
		a.entries = append(a.entries,
			fmEntry{name: "终端", path: "@terminal", kind: ui.IcoTerminal, typ: "程序", virtual: true},
			fmEntry{name: "网络浏览器", path: "@browser", kind: ui.IcoBrowser, typ: "程序", virtual: true},
		)
	} else {
		ents, err := os.ReadDir(a.path)
		if err != nil {
			a.loadErr = err.Error()
		} else {
			for _, de := range ents {
				name := de.Name()
				hidden := strings.HasPrefix(name, ".")
				if hidden && !a.showHidden {
					continue
				}
				info, err := de.Info()
				if err != nil {
					continue
				}
				full := filepath.Join(a.path, name)
				isDir := de.IsDir()
				isLink := de.Type()&os.ModeSymlink != 0
				if isLink {
					if st, err := os.Stat(full); err == nil {
						isDir = st.IsDir()
					}
				}
				kind, typ := fmClassify(name)
				if isDir {
					kind, typ = ui.IcoFolder, "文件夹"
					if isLink {
						typ = "文件夹快捷方式"
					}
				} else if isLink {
					typ = "快捷方式 → " + typ
				}
				a.entries = append(a.entries, fmEntry{
					name: name, path: full, isDir: isDir,
					kind: kind, size: info.Size(), mod: info.ModTime(), typ: typ,
				})
			}
		}
		sort.SliceStable(a.entries, func(i, j int) bool {
			if a.entries[i].isDir != a.entries[j].isDir {
				return a.entries[i].isDir
			}
			return strings.ToLower(a.entries[i].name) < strings.ToLower(a.entries[j].name)
		})
	}
	if keep {
		a.sel = psel
		a.scroll.Pos = pscroll
	} else {
		a.sel = -1
		a.scroll.Pos = 0
	}
	if len(a.entries) == 0 {
		a.sel = -1
	} else if a.sel >= len(a.entries) {
		a.sel = len(a.entries) - 1
	}
	if a.scroll.Pos < 0 {
		a.scroll.Pos = 0
	}
	a.scroll.Total = len(a.entries)
	a.scroll.Clamp()
	a.saveCurrent()
}

func (a *FileManagerApp) saveCurrent() {
	if a.tab < 0 || a.tab >= len(a.tabs) {
		return
	}
	a.tabs[a.tab] = fmTabState{path: a.path, sel: a.sel, scroll: a.scroll.Pos}
}

func (a *FileManagerApp) switchTab(i int) {
	if i < 0 || i >= len(a.tabs) || i == a.tab {
		return
	}
	a.saveCurrent()
	a.tab = i
	st := a.tabs[a.tab]
	a.path, a.sel, a.scroll.Pos = st.path, st.sel, st.scroll
	a.lastClick = time.Time{}
	a.lastSel = -1
	a.reload(true)
}

func (a *FileManagerApp) newTab() {
	a.saveCurrent()
	st := fmTabState{path: a.path, sel: -1}
	a.tabs = append(a.tabs, st)
	a.tab = len(a.tabs) - 1
	a.path = st.path
	a.sel = -1
	a.scroll.Pos = 0
	a.lastClick = time.Time{}
	a.lastSel = -1
	a.reload(false)
}

func (a *FileManagerApp) closeTab() { a.closeTabAt(a.tab) }

func (a *FileManagerApp) closeTabAt(i int) {
	if len(a.tabs) <= 1 || i < 0 || i >= len(a.tabs) {
		return
	}
	a.saveCurrent()
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if a.tab > i {
		a.tab--
	} else if a.tab >= len(a.tabs) {
		a.tab = len(a.tabs) - 1
	}
	st := a.tabs[a.tab]
	a.path, a.sel, a.scroll.Pos = st.path, st.sel, st.scroll
	a.lastClick = time.Time{}
	a.lastSel = -1
	a.reload(true)
}

func (a *FileManagerApp) nextTab(delta int) {
	if len(a.tabs) < 2 {
		return
	}
	i := (a.tab + delta) % len(a.tabs)
	if i < 0 {
		i += len(a.tabs)
	}
	a.switchTab(i)
}

func (a *FileManagerApp) toggleHidden(d *Desktop) {
	a.showHidden = !a.showHidden
	a.load()
	d.Invalidate()
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
	a.btnUp = ui.Rect{tx, ty, 62, 24}
	a.btnNew = ui.Rect{tx + 66, ty, 80, 24}
	a.btnDelete = ui.Rect{tx + 150, ty, 52, 24}
	a.btnTerm = ui.Rect{tx + 206, ty, 52, 24}
	a.btnHidden = ui.Rect{tx + 262, ty, 76, 24}
	ui.Button(c, a.btnUp, "上一级", a.press == "up", a.path != "")
	ui.Button(c, a.btnNew, "新建文件夹", a.press == "new", a.path != "" && a.path != trashDir)
	ui.Button(c, a.btnDelete, "删除", a.press == "del", a.sel >= 0)
	ui.Button(c, a.btnTerm, "终端", a.press == "term", true)
	ui.Button(c, a.btnHidden, "隐藏文件", a.press == "hidden" || a.showHidden, true)
	c.HLine(r.X+4, r.Y+31, r.W-8, ui.ColShadow)
	c.HLine(r.X+4, r.Y+32, r.W-8, ui.ColWhite)

	a.tabBarR = ui.Rect{r.X + 4, r.Y + 35, r.W - 8, 24}
	a.paintTabs(c, f, a.tabBarR)

	addr := ui.Rect{r.X + 6, a.tabBarR.Bottom() + 3, r.W - 12, 24}
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
		if !e.mod.IsZero() {
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
	if a.showHidden {
		info += "　·　显示隐藏文件"
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, info, ui.ColText)
}

func (a *FileManagerApp) paintTabs(c *ui.Canvas, f *ui.Font, bar ui.Rect) {
	c.FillRect(bar, ui.ColFace)
	a.tabRects = a.tabRects[:0]
	a.tabCloseRects = a.tabCloseRects[:0]
	plusW := 24
	tw := 170
	if n := len(a.tabs); n > 0 {
		if fit := (bar.W - plusW - 4) / n; fit < tw {
			tw = fit
		}
	}
	if tw < 96 {
		tw = 96
	}
	x := bar.X
	for i := 0; i < len(a.tabs); i++ {
		if x+tw > bar.Right()-plusW-2 {
			break
		}
		tr := ui.Rect{x, bar.Y, tw - 2, bar.H}
		if i == a.tab {
			c.FillRect(tr, ui.ColWhite)
			c.Raised(tr)
		} else {
			c.FillRect(tr, ui.ColFace)
			c.Sunken(tr)
		}
		f.TextClip(c, tr.X+6, tr.Y+4, tr.W-24, fmPathLabel(a.tabs[i].path), ui.ColText)
		cr := ui.Rect{tr.Right() - 16, tr.Y + 3, 14, 16}
		f.Text(c, cr.X+3, cr.Y+1, "×", ui.ColText)
		a.tabRects = append(a.tabRects, tr)
		a.tabCloseRects = append(a.tabCloseRects, cr)
		x += tw
	}
	a.tabPlusR = ui.Rect{x, bar.Y + 1, plusW, bar.H - 2}
	ui.Button(c, a.tabPlusR, "+", a.press == "tabnew", true)
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
		case a.btnTerm.Contains(ev.X, ev.Y):
			a.press = "term"
		case a.btnHidden.Contains(ev.X, ev.Y):
			a.press = "hidden"
		case a.tabBarR.Contains(ev.X, ev.Y):
			if a.tabPlusR.Contains(ev.X, ev.Y) {
				a.press = "tabnew"
				break
			}
			for i, cr := range a.tabCloseRects {
				if cr.Contains(ev.X, ev.Y) {
					a.press = "tabclose:" + strconv.Itoa(i)
					break
				}
			}
			if a.press == "" {
				for i, tr := range a.tabRects {
					if tr.Contains(ev.X, ev.Y) {
						a.press = "tab:" + strconv.Itoa(i)
						break
					}
				}
			}
		case a.listR.Contains(ev.X, ev.Y):
			idx := a.rowAt(ev.X, ev.Y)
			if idx >= 0 {
				a.sel = idx
				e := a.entries[idx]
				if e.path != "" && !strings.HasPrefix(e.path, "@") && !e.virtual {
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
		switch {
		case p == "up":
			if a.btnUp.Contains(ev.X, ev.Y) {
				a.up()
				d.Invalidate()
			}
		case p == "new":
			if a.btnNew.Contains(ev.X, ev.Y) {
				a.newFolder(d)
			}
		case p == "del":
			if a.btnDelete.Contains(ev.X, ev.Y) {
				a.deleteSelected(d)
			}
		case p == "term":
			if a.btnTerm.Contains(ev.X, ev.Y) {
				a.openTerminalHere(d)
			}
		case p == "hidden":
			if a.btnHidden.Contains(ev.X, ev.Y) {
				a.toggleHidden(d)
			}
		case p == "tabnew":
			a.newTab()
			d.Invalidate()
		case strings.HasPrefix(p, "tabclose:"):
			if idx, err := strconv.Atoi(strings.TrimPrefix(p, "tabclose:")); err == nil {
				a.closeTabAt(idx)
				d.Invalidate()
			}
		case strings.HasPrefix(p, "tab:"):
			if idx, err := strconv.Atoi(strings.TrimPrefix(p, "tab:")); err == nil {
				a.switchTab(idx)
				d.Invalidate()
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
	case "@archterm":
		a.openArchTerminal()
		return
	}
	if e.isDir {
		a.path = e.path
		a.load()
		return
	}
	lower := strings.ToLower(e.name)
	ext := filepath.Ext(lower)
	if strings.HasSuffix(lower, ".desktop") {
		if sc, ok := parseShortcut(e.path); ok {
			if execLine := cleanExec(sc.Exec); execLine != "" {
				a.d.launchShortcut(execLine, sc.Terminal)
				return
			}
		}
		a.d.MessageBox("文件管理器", "无法读取快捷方式“"+e.name+"”。", IconWarning, []string{"确定"}, nil)
		return
	}
	switch {
	case ext == ".html" || ext == ".htm" || ext == ".xhtml" || ext == ".shtml":
		a.d.openBrowser("file://" + e.path)
	case ext == ".png" || ext == ".bmp" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif":
		a.d.openImageViewer(e.path)
	case ext == ".sh":
		a.d.runScript(e.path, "/bin/sh")
	case ext == ".bash":
		a.d.runScript(e.path, "/bin/bash")
	case fmArchiveExt(ext):
		a.openArchive(e)
	case officeFileModule(e.name) != "":
		a.d.openOfficeFile(e.path)
	case fmTextType(e.name):
		a.d.openNotepad(e.path)
	default:
		a.d.MessageBox("文件管理器", "无法打开“"+e.name+"”。\n\n"+
			"类型："+e.typ+"\n大小："+sizeText(e.size)+"\n路径："+e.path+"\n\n"+
			"没有与此文件类型关联的程序。可安装 Office 365 全件套编辑办公文档，\n或者双击打开文本、源代码、配置、图片、HTML 和脚本文件。", IconInfo, []string{"确定"}, nil)
	}
}

func (a *FileManagerApp) openTerminalHere(d *Desktop) {
	if a.inArch() {
		a.openArchTerminal()
		return
	}
	dir := a.path
	switch dir {
	case "":
		dir = "/root"
	case trashDir:
		dir = "/tmp"
	}
	d.openTerminalAt(dir)
}

func (a *FileManagerApp) openArchTerminal() {
	if !fmArchInstalled() {
		a.d.MessageBox("Arch Linux 子系统",
			"还没有安装 Arch Linux 子系统。\n\n请从开始菜单运行“Arch Linux (glibc / pacman)”完成安装后再试。",
			IconInfo, []string{"确定"}, nil)
		return
	}
	a.d.openTerminalCommand("Arch Linux 子系统", []string{"/usr/bin/win95-arch-run", "bash"}, "/", "arch-shell")
}

func (a *FileManagerApp) openArchive(e fmEntry) {
	dir := filepath.Dir(e.path)
	script := "cd " + fmShellQuote(dir) + "\n" +
		"echo " + fmShellQuote("=== "+e.name+" ===") + "\n" +
		"(tar -tf " + fmShellQuote(e.name) + " 2>/dev/null || unzip -l " + fmShellQuote(e.name) + " 2>/dev/null || bsdtar -tf " + fmShellQuote(e.name) + " 2>/dev/null || ls -l " + fmShellQuote(e.name) + ")\n" +
		"exec /bin/sh"
	a.d.openTerminalCommand("终端 - "+e.name, []string{"/bin/sh", "-c", script}, dir, "archive:"+e.path)
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

func (a *FileManagerApp) createShortcut(d *Desktop) {
	e, ok := a.selectedReal()
	if !ok {
		return
	}
	if a.path == trashDir {
		d.MessageBox("创建快捷方式", "不能在回收站里创建快捷方式。", IconWarning, []string{"确定"}, nil)
		return
	}
	ext := filepath.Ext(e.name)
	stem := strings.TrimSuffix(e.name, ext)
	base := stem + " - 快捷方式"
	name := base + ext
	var target string
	for i := 2; ; i++ {
		target = filepath.Join(a.path, name)
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	if err := os.Symlink(e.path, target); err != nil {
		d.MessageBox("创建快捷方式", "无法创建快捷方式：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a.load()
	for i, ent := range a.entries {
		if ent.path == target {
			a.sel = i
			break
		}
	}
	d.Invalidate()
}

func (a *FileManagerApp) deleteSelected(d *Desktop) {
	if a.sel < 0 || a.sel >= len(a.entries) {
		return
	}
	e := a.entries[a.sel]
	if e.virtual || protectedPath(e.path) || e.path == "" || strings.HasPrefix(e.path, "@") {
		d.MessageBox("文件管理器", "不能删除“"+e.name+"”。", IconWarning, []string{"确定"}, nil)
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
			if _, err := moveToTrash(e.path); err != nil {
				d.MessageBox("文件管理器", "删除失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			a.load()
			d.Invalidate()
		})
}

func (a *FileManagerApp) renameSelected(d *Desktop) {
	e, ok := a.selectedReal()
	if !ok {
		return
	}
	if protectedPath(e.path) {
		d.MessageBox("重命名", "不能重命名“"+e.name+"”。", IconWarning, []string{"确定"}, nil)
		return
	}
	dir := a.path
	d.InputBox("重命名", "输入新名称：", e.name, func(name string, ok bool) {
		if !ok {
			return
		}
		name = strings.TrimSpace(name)
		if name == "" || name == e.name {
			return
		}
		target := filepath.Join(dir, name)
		if _, err := os.Lstat(target); err == nil {
			d.MessageBox("重命名", "已经存在名为“"+name+"”的项目。\n\n请换一个名称。", IconWarning, []string{"确定"}, nil)
			return
		}
		if err := os.Rename(e.path, target); err != nil {
			d.MessageBox("重命名", "无法重命名：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		a.load()
		for i, ent := range a.entries {
			if ent.path == target {
				a.sel = i
				break
			}
		}
		d.Invalidate()
	})
}

func (a *FileManagerApp) ContextMenu(d *Desktop, w *Window, x, y int) []ctxItem {
	if !a.listR.Contains(x, y) {
		return nil
	}
	idx := a.rowAt(x, y)
	if idx >= 0 {
		a.sel = idx
		d.Invalidate()
	}
	inTrash := a.path == trashDir
	var items []ctxItem
	if idx >= 0 {
		e := a.entries[idx]
		real := e.path != "" && !strings.HasPrefix(e.path, "@") && !e.virtual
		renamable := real && !protectedPath(e.path)
		items = append(items,
			ctxItem{label: "打开(O)", icon: e.kind, enabled: true, action: func() { a.activate(idx) }},
			ctxItem{label: a.terminalLabel(), icon: ui.IcoTerminal, enabled: true, action: func() { a.openTerminalHere(d) }},
			ctxItem{label: "创建快捷方式(S)", icon: ui.IcoFile, enabled: real && !inTrash, action: func() { a.createShortcut(d) }},
			ctxItem{label: "重命名(M)", enabled: renamable, action: func() { a.renameSelected(d) }},
		)
		if inTrash {
			items = append(items, ctxItem{label: "永久删除(D)", icon: ui.IcoRecycle, enabled: true, action: func() { a.deleteSelected(d) }})
		} else {
			items = append(items, ctxItem{label: "删除(D)", icon: ui.IcoRecycle, enabled: real, action: func() { a.deleteSelected(d) }})
		}
		items = append(items, ctxItem{sep: true})
	}
	items = append(items,
		ctxItem{label: "新建文件夹(N)", icon: ui.IcoFolder, enabled: a.path != "" && !inTrash, action: func() { a.newFolder(d) }},
		ctxItem{label: "新建标签页(T)", accel: "Ctrl+T", enabled: true, action: func() { a.newTab(); d.Invalidate() }},
		ctxItem{label: "刷新(R)", accel: "F5", icon: ui.IcoInfo, enabled: true, action: func() { a.reload(true); d.Invalidate() }},
		ctxItem{label: "显示隐藏文件(H)", accel: "Ctrl+H", icon: ui.IcoInfo, checked: a.showHidden, enabled: true, action: func() { a.toggleHidden(d) }},
	)
	if inTrash && d.recycleCount() > 0 {
		items = append(items, ctxItem{sep: true}, ctxItem{label: "清空回收站(E)", icon: ui.IcoRecycle, enabled: true, action: func() { d.emptyRecycle(a.load) }})
	}
	return items
}

func (a *FileManagerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl {
		switch ev.Name {
		case "T":
			a.newTab()
			d.Invalidate()
			return true
		case "W":
			a.closeTab()
			d.Invalidate()
			return true
		case "H":
			a.toggleHidden(d)
			return true
		case "N":
			a.newFolder(d)
			return true
		case "Tab":
			if ev.Shift {
				a.nextTab(-1)
			} else {
				a.nextTab(1)
			}
			d.Invalidate()
			return true
		}
	}
	switch ev.Name {
	case "Enter":
		a.activate(a.sel)
		return true
	case "F2":
		a.renameSelected(d)
		return true
	case "Backspace":
		a.up()
		d.Invalidate()
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
		a.reload(true)
		d.Invalidate()
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
			a.ensureSel()
			d.Invalidate()
		}
		return true
	case "Down":
		if a.sel < len(a.entries)-1 {
			a.sel++
			a.ensureSel()
			d.Invalidate()
		}
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

func fmArchInstalled() bool {
	st, err := os.Stat(filepath.Join(archRootDir, "usr/bin/pacman"))
	return err == nil && !st.IsDir()
}

func fmSharedMounts() []fmShare {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []fmShare
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		fstype := fields[2]
		if fstype != "virtiofs" && fstype != "9p" && !strings.Contains(fstype, "virtiofs") && !strings.HasPrefix(fstype, "9p") {
			continue
		}
		mount := fmUnescapeMount(fields[1])
		if seen[mount] {
			continue
		}
		seen[mount] = true
		out = append(out, fmShare{source: fmUnescapeMount(fields[0]), mount: mount, fstype: fstype})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mount < out[j].mount })
	return out
}

func fmUnescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func fmShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func fmArchiveExt(ext string) bool {
	switch ext {
	case ".zip", ".tar", ".gz", ".tgz", ".xz", ".txz", ".bz2", ".tbz2", ".zst", ".tzst", ".7z", ".rar", ".cab", ".deb", ".rpm", ".apk":
		return true
	}
	return false
}

func fmTextType(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".txt", ".md", ".markdown", ".rst", ".log", ".nfo",
		".conf", ".cfg", ".ini", ".json", ".yaml", ".yml", ".toml", ".xml", ".plist",
		".service", ".env", ".properties", ".list", ".lock", ".sum", ".csv", ".tsv",
		".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".cs", ".go", ".rs", ".py",
		".rb", ".pl", ".php", ".lua", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx",
		".css", ".scss", ".sass", ".less", ".sql", ".vim", ".diff", ".patch", ".mk":
		return true
	}
	switch strings.ToLower(filepath.Base(name)) {
	case "makefile", "dockerfile", "containerfile", "license", "readme", "changelog", "authors":
		return true
	}
	return false
}

func fmClassify(name string) (ui.IconKind, string) {
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)
	switch ext {
	case ".desktop":
		return ui.IcoSetup, "应用程序快捷方式"
	case ".html", ".htm", ".xhtml", ".shtml":
		return ui.IcoBrowser, "HTML 文档"
	case ".png":
		return ui.IcoImage, "PNG 图片"
	case ".bmp":
		return ui.IcoImage, "BMP 图片"
	case ".jpg", ".jpeg":
		return ui.IcoImage, "JPEG 图片"
	case ".gif":
		return ui.IcoImage, "GIF 图片"
	case ".webp", ".tif", ".tiff", ".ico", ".svg", ".heic", ".avif":
		return ui.IcoImage, "图片文件"
	case ".sh":
		return ui.IcoTerminal, "Shell 脚本"
	case ".bash":
		return ui.IcoTerminal, "Bash 脚本"
	case ".zsh", ".fish", ".ps1", ".bat", ".cmd":
		return ui.IcoTerminal, "脚本文件"
	case ".zip", ".tar", ".gz", ".tgz", ".xz", ".txz", ".bz2", ".tbz2", ".zst", ".tzst", ".7z", ".rar", ".cab":
		return ui.IcoPackage, "压缩文件"
	case ".deb", ".rpm", ".apk", ".pkg", ".flatpak", ".appimage":
		return ui.IcoPackage, "软件包"
	case ".pdf", ".djvu", ".epub", ".mobi":
		return ui.IcoText, "电子文档"
	case ".doc", ".docx", ".docm", ".dot", ".dotx", ".xls", ".xlsx", ".xlsm",
		".ppt", ".pptx", ".pptm", ".pps", ".ppsx", ".odt", ".ods", ".odp",
		".odg", ".odf", ".odb", ".rtf", ".csv", ".tsv", ".vsd", ".vsdx":
		return ui.IcoOffice, "办公文档"
	case ".mp3", ".wav", ".flac", ".ogg", ".oga", ".m4a", ".aac", ".opus", ".wma",
		".mp4", ".mkv", ".avi", ".mov", ".webm", ".wmv", ".flv", ".m4v", ".mpg", ".mpeg":
		return ui.IcoFile, "媒体文件"
	case ".ttf", ".otf", ".woff", ".woff2", ".ttc":
		return ui.IcoFile, "字体文件"
	case ".iso", ".img", ".raw", ".qcow2", ".vdi", ".vmdk", ".vhdx", ".dmg":
		return ui.IcoDrive, "磁盘映像"
	case ".exe", ".msi", ".dll", ".sys", ".ocx":
		return ui.IcoSetup, "Windows 程序"
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".cs", ".go", ".rs", ".py",
		".rb", ".pl", ".php", ".lua", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx",
		".css", ".scss", ".sass", ".less", ".sql", ".vim", ".diff", ".patch", ".mk":
		return ui.IcoText, "源代码"
	case ".conf", ".cfg", ".ini", ".json", ".yaml", ".yml", ".toml", ".xml", ".plist",
		".service", ".env", ".properties", ".list", ".lock", ".sum":
		return ui.IcoText, "配置文件"
	}
	if fmTextType(name) {
		return ui.IcoText, "文本文档"
	}
	return ui.IcoFile, "文件"
}
