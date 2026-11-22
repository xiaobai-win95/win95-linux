package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

type officeTile struct {
	rect ui.Rect
	id   string
}

// OfficeApp is the suite launcher: one tile per installed application, plus a
// component manager that doubles as the add-on installer.
type OfficeApp struct {
	Base
	d   *Desktop
	win *Window

	installed   map[string]bool
	lastRefresh time.Time
	hover       string
	press       string
	tiles       []officeTile

	btnManage, btnDocs, btnClose ui.Rect
	footer                       int // footer top, set by Paint for hit tests
}

func NewOfficeApp(d *Desktop) *OfficeApp {
	return &OfficeApp{d: d, installed: officeInstalledPackages()}
}

func (a *OfficeApp) Title() string     { return "Office 365 全件套" }
func (a *OfficeApp) Icon() ui.IconKind { return ui.IcoOffice }

func (a *OfficeApp) DefaultSize() (int, int) { return 860, 620 }
func (a *OfficeApp) MinSize() (int, int)     { return 720, 500 }
func (a *OfficeApp) Resizable() bool         { return true }

func (a *OfficeApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "打开文档(O)…", Enabled: true, Action: func() {
				d.FileDialog(FileOpen, "/root/文档", "", func(path string, ok bool) {
					if ok {
						d.openOfficeFile(path)
					}
				})
			}},
			{Label: "我的文档文件夹(D)", Enabled: true, Action: func() { d.openFiles("/root/文档") }},
			{Separator: true},
			{Label: "关闭(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "组件(C)", Items: []MenuItem{
			{Label: "选择安装 / 补装组件(M)…", Enabled: true, Action: func() { d.openOfficeSetup("", "", "") }},
			{Label: "刷新安装状态(F5)", Enabled: true, Action: func() {
				a.refreshInstalled()
				d.Invalidate()
			}},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于 Office 365 全件套", Enabled: true, Action: func() { a.showAbout() }},
		}},
	}
}

func (a *OfficeApp) showAbout() {
	a.d.MessageBox("关于 Office 365 全件套",
		"Windows 95 Linux Office 365 全件套\n版本 26.5\n\n"+
			"文档引擎：LibreOffice 25.8（Alpine 官方 aarch64 构建）\n"+
			"PDF：Zathura + poppler-utils + qpdf\n"+
			"输入法：fcitx5 拼音\n\n"+
			"组件按需安装，不占用系统镜像体积；安装后可在“组件管理”中补装。\n"+
			"办公文档由 X11 嵌套代理显示，最大化窗口可获得更完整的编辑区域。",
		IconInfo, []string{"确定"}, nil)
}

func (a *OfficeApp) refreshInstalled() bool {
	next := officeInstalledPackages()
	changed := len(next) != len(a.installed)
	if !changed {
		for k := range next {
			if !a.installed[k] {
				changed = true
				break
			}
		}
	}
	a.installed = next
	return changed
}

func (a *OfficeApp) installedApps() int {
	n := 0
	for i := range officeModules {
		if a.moduleInstalled(&officeModules[i]) {
			n++
		}
	}
	return n
}

func (a *OfficeApp) moduleInstalled(m *officeModule) bool {
	comp := officeCompByID(m.Comp)
	return comp != nil && officeCompInstalled(*comp, a.installed)
}

func (a *OfficeApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if now.Sub(a.lastRefresh) > 2*time.Second {
		a.lastRefresh = now
		if a.refreshInstalled() {
			d.Invalidate()
		}
	}
}

func (a *OfficeApp) layout(r ui.Rect) {
	a.footer = r.Bottom() - 42
	a.btnClose = ui.Rect{r.Right() - 14 - 78, r.Bottom() - 34, 78, 26}
	a.btnDocs = ui.Rect{a.btnClose.X - 8 - 96, r.Bottom() - 34, 96, 26}
	a.btnManage = ui.Rect{a.btnDocs.X - 8 - 112, r.Bottom() - 34, 112, 26}

	items := len(officeModules) + 1
	cols := 4
	if r.W < 760 {
		cols = 3
	}
	rows := (items + cols - 1) / cols
	pad := 14
	gap := 10
	top := r.Y + 80
	bottom := a.footer - 10
	availH := bottom - top
	tileH := (availH - (rows-1)*gap) / rows
	if tileH > 116 {
		tileH = 116
	}
	if tileH < 84 {
		tileH = 84
	}
	tileW := (r.W - 2*pad - (cols-1)*gap) / cols
	if tileW < 120 {
		tileW = 120
	}
	a.tiles = a.tiles[:0]
	for i := 0; i < items; i++ {
		col := i % cols
		row := i / cols
		x := r.X + pad + col*(tileW+gap)
		y := top + row*(tileH+gap)
		id := "@manage"
		if i < len(officeModules) {
			id = officeModules[i].ID
		}
		a.tiles = append(a.tiles, officeTile{rect: ui.Rect{x, y, tileW, tileH}, id: id})
	}
}

func (a *OfficeApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	a.layout(r)
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	header := ui.Rect{r.X, r.Y, r.W, 66}
	c.TitleBar(header, 0x1F3864, 0x2F5496)
	ui.FlagLogo(c, header.X+18, header.Y+18, 30)
	f.TextBold(c, header.X+60, header.Y+12, "Office 365 全件套", ui.ColWhite)
	f.Text(c, header.X+60, header.Y+36, "Word · Excel · PowerPoint · PDF · 邮件 · 笔记", 0xC8DCFF)
	f.TextRight(c, header.Right()-16, header.Y+12, fmt.Sprintf("已安装 %d / %d 个应用", a.installedApps(), len(officeModules)), 0xC8DCFF)
	core := officeCompByID("core")
	if core != nil && !officeCompInstalled(*core, a.installed) {
		f.TextRight(c, header.Right()-16, header.Y+36, "首次使用：请打开“组件管理”选择并安装", 0xFFE699)
	} else {
		f.TextRight(c, header.Right()-16, header.Y+36, "Windows 95 Linux 26.5", 0xC8DCFF)
	}

	for _, t := range a.tiles {
		if t.id == "@manage" {
			a.paintManageTile(c, f, t.rect)
			continue
		}
		m := officeModuleByID(t.id)
		if m == nil {
			continue
		}
		a.paintModuleTile(c, f, t.rect, m)
	}

	summary := "文档保存在 /root/文档，双击办公文档会用对应应用打开。"
	if !a.installed["libreoffice-common"] {
		summary = "尚未安装 Office 核心组件。打开“组件管理”(M) 选择需要的应用。"
	}
	f.TextClip(c, r.X+16, r.Bottom()-27, r.W-16-420, summary, ui.ColShadow)

	ui.Button(c, a.btnManage, "组件管理(M)", a.press == "@manage", true)
	ui.Button(c, a.btnDocs, "我的文档(D)", a.press == "@docs", true)
	ui.Button(c, a.btnClose, "关闭", a.press == "@close", true)
}

func (a *OfficeApp) paintModuleTile(c *ui.Canvas, f *ui.Font, r ui.Rect, m *officeModule) {
	installed := a.moduleInstalled(m)
	hover := a.hover == m.ID
	c.FillRect(r, ui.ColFace)
	if hover {
		c.FillRect(r, 0xEEF4FC)
	}
	c.Raised(r)
	if a.press == m.ID {
		c.Sunken(r)
	}
	band := m.Color
	if !installed {
		band = 0x8A8A8A
	}
	c.FillRect(ui.Rect{r.X + 2, r.Y + 2, r.W - 4, 26}, band)
	letter := m.Letter
	f.TextBold(c, r.X+(r.W-f.Width(letter))/2, r.Y+7, letter, ui.ColWhite)
	col := ui.ColText
	if !installed {
		col = ui.ColShadow
	}
	f.TextBold(c, r.X+8, r.Y+34, m.Name, col)
	if r.H >= 100 {
		f.TextClip(c, r.X+8, r.Y+53, r.W-16, m.Desc, ui.ColShadow)
	}
	if installed {
		f.Text(c, r.X+8, r.Bottom()-20, "已安装", 0x1B7A2E)
	} else {
		f.Text(c, r.X+8, r.Bottom()-20, "未安装 · 点击安装", ui.ColShadow)
	}
	if hover {
		c.Frame(r, ui.ColNavy)
	}
}

func (a *OfficeApp) paintManageTile(c *ui.Canvas, f *ui.Font, r ui.Rect) {
	missing := 0
	for _, comp := range officeComponents {
		if !officeCompInstalled(comp, a.installed) {
			missing++
		}
	}
	c.FillRect(r, ui.ColFace)
	if a.hover == "@manage" {
		c.FillRect(r, 0xEEF4FC)
	}
	c.Raised(r)
	if a.press == "@manage" {
		c.Sunken(r)
	}
	c.FillRect(ui.Rect{r.X + 2, r.Y + 2, r.W - 4, 26}, 0x4A5A6A)
	f.TextBold(c, r.X+(r.W-f.Width("+"))/2, r.Y+7, "+", ui.ColWhite)
	f.TextBold(c, r.X+8, r.Y+34, "组件管理", ui.ColText)
	if r.H >= 100 {
		f.TextClip(c, r.X+8, r.Y+53, r.W-16, "选择安装或补装未安装的组件", ui.ColShadow)
	}
	status := "全部组件已安装"
	if missing > 0 {
		status = fmt.Sprintf("还有 %d 个组件可安装", missing)
	}
	f.Text(c, r.X+8, r.Bottom()-20, status, ui.ColShadow)
	if a.hover == "@manage" {
		c.Frame(r, ui.ColNavy)
	}
}

func (a *OfficeApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	a.layout(client)
	if ev.Kind == MouseMove {
		hover := ""
		for _, t := range a.tiles {
			if t.rect.Contains(ev.X, ev.Y) {
				hover = t.id
				break
			}
		}
		if a.btnManage.Contains(ev.X, ev.Y) {
			hover = "@manage"
		} else if a.btnDocs.Contains(ev.X, ev.Y) {
			hover = "@docs"
		} else if a.btnClose.Contains(ev.X, ev.Y) {
			hover = "@close"
		}
		if hover != a.hover {
			a.hover = hover
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
		for _, t := range a.tiles {
			if t.rect.Contains(ev.X, ev.Y) {
				a.press = t.id
				break
			}
		}
		switch {
		case a.press != "":
		case a.btnManage.Contains(ev.X, ev.Y):
			a.press = "@manage"
		case a.btnDocs.Contains(ev.X, ev.Y):
			a.press = "@docs"
		case a.btnClose.Contains(ev.X, ev.Y):
			a.press = "@close"
		}
		if a.press != "" {
			d.Invalidate()
		}
	case MouseUp:
		p := a.press
		a.press = ""
		if p != "" {
			switch p {
			case "@manage":
				if a.btnManage.Contains(ev.X, ev.Y) || tileRect(a.tiles, p).Contains(ev.X, ev.Y) {
					d.openOfficeSetup("", "", "")
				}
			case "@docs":
				if a.btnDocs.Contains(ev.X, ev.Y) {
					d.openFiles("/root/文档")
				}
			case "@close":
				if a.btnClose.Contains(ev.X, ev.Y) {
					d.CloseWindow(w)
				}
			default:
				if tileRect(a.tiles, p).Contains(ev.X, ev.Y) {
					d.openOfficeModule(p, "")
				}
			}
			d.Invalidate()
		}
	}
}

func tileRect(tiles []officeTile, id string) ui.Rect {
	for _, t := range tiles {
		if t.id == id {
			return t.rect
		}
	}
	return ui.Rect{}
}

func (a *OfficeApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl || ev.Alt {
		return false
	}
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "F5":
		if a.refreshInstalled() {
			d.Invalidate()
		}
		return true
	}
	return false
}

// openOffice opens (or reuses) the suite launcher.
func (d *Desktop) openOffice() *OfficeApp {
	for _, w := range d.windows {
		if app, ok := w.App.(*OfficeApp); ok {
			d.bringToFront(w)
			app.refreshInstalled()
			d.Invalidate()
			return app
		}
	}
	app := NewOfficeApp(d)
	w := d.Open(app)
	app.win = w
	return app
}

// openOfficeModule launches a suite application inside the X11 proxy, or asks
// the user to install the component that provides it.
func (d *Desktop) openOfficeModule(moduleID, path string) {
	m := officeModuleByID(moduleID)
	if m == nil {
		return
	}
	if !officeModuleAvailable(m) {
		d.openOfficeSetup(m.Comp, m.ID, path)
		return
	}
	argv := officeModuleArgv(m, path)
	if len(argv) == 0 {
		d.openOfficeSetup(m.Comp, m.ID, path)
		return
	}
	d.openOfficeProxy(argv)
}

// openOfficeFile routes a double-clicked document to its Office module.
func (d *Desktop) openOfficeFile(path string) {
	moduleID := officeFileModule(path)
	if moduleID == "" {
		d.MessageBox("Office 365 全件套", "没有为这个文件类型找到 Office 应用。", IconInfo, []string{"确定"}, nil)
		return
	}
	d.openOfficeModule(moduleID, path)
}

// openOfficeProxy reuses the nested X11 window, maximizes it for document
// editing and forwards the command line to the virtual display.
func (d *Desktop) openOfficeProxy(argv []string) {
	px := d.x11ProxyWindow("", nil)
	if px == nil {
		return
	}
	if px.win != nil {
		d.bringToFront(px.win)
		d.maximizeWindow(px.win)
	}
	if len(argv) > 0 {
		px.launchArgv(argv)
	}
}
