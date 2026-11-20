package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/ui"
)

const (
	officeSetupPIDPath = "/run/win95-office-setup.pid"
	officeRowH         = 46
)

// processAlive reports whether pid exists (used to detect a crashed installer).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// OfficeSetupApp lets the user pick which parts of the Office suite to install
// now and which to add later. Installed components are detected from the apk
// database, so reopening the window doubles as the add-on installer.
type OfficeSetupApp struct {
	Base
	d   *Desktop
	win *Window

	installed map[string]bool
	sel       map[string]bool
	scroll    VScroll

	installing     bool
	installStarted time.Time
	installMsg     string
	pendingModule  string
	pendingPath    string
	reveal         string
	msg            string

	press    string
	rows     []ui.Rect
	listR    ui.Rect
	btnRec   ui.Rect
	btnAll   ui.Rect
	btnInst  ui.Rect
	btnProg  ui.Rect
	btnClose ui.Rect

	lastRefresh time.Time
	lastPoll    time.Time
}

func NewOfficeSetupApp(d *Desktop, pendingComp, pendingModule, pendingPath string) *OfficeSetupApp {
	a := &OfficeSetupApp{d: d, installed: officeInstalledPackages()}
	a.sel = officeSelectionDefaults(a.installed)
	if pendingComp != "" {
		a.sel[pendingComp] = true
		a.reveal = pendingComp
		if c := officeCompByID(pendingComp); c != nil {
			a.msg = "要先安装“" + c.Name + "”才能完成刚才的操作。"
		}
	}
	a.pendingModule = pendingModule
	a.pendingPath = pendingPath
	if pendingPath != "" && a.msg == "" {
		a.msg = "打开该文档需要先安装对应的 Office 组件。"
	}
	return a
}

func (a *OfficeSetupApp) Title() string     { return "Office 365 全件套 - 选择组件" }
func (a *OfficeSetupApp) Icon() ui.IconKind { return ui.IcoOffice }

func (a *OfficeSetupApp) DefaultSize() (int, int) { return 780, 600 }
func (a *OfficeSetupApp) MinSize() (int, int)     { return 680, 480 }
func (a *OfficeSetupApp) Resizable() bool         { return true }

func (a *OfficeSetupApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "关闭(X)", Enabled: !a.installing, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "组件(C)", Items: []MenuItem{
			{Label: "推荐选择(R)", Enabled: !a.installing, Action: func() { a.recommend() }},
			{Label: "全选(A)", Enabled: !a.installing, Action: func() { a.selectAll() }},
			{Separator: true},
			{Label: "安装所选(I)", Enabled: !a.installing, Action: func() { a.installSelected() }},
			{Label: "查看安装进度(P)", Enabled: a.installing, Action: func() { d.openInstallStatus("Office 365 全件套") }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于 Office 365 全件套", Enabled: true, Action: func() {
				d.MessageBox("关于 Office 365 全件套",
					"Windows 95 Linux Office 365 全件套\n\n"+
						"以 Alpine 官方 LibreOffice 为核心，按需安装文档、表格、演示、绘图、\n"+
						"公式、数据库、PDF、邮件与中文输入法。\n\n"+
						"组件不会随系统镜像打包，首次使用时从 Alpine 软件源下载；\n"+
						"安装完成后随时可以再次打开本窗口补装缺少的组件。\n\n"+
						"文档通过 X11 嵌套代理显示在 Win95 窗口中，并已配置中文输入法。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *OfficeSetupApp) refreshInstalled() bool {
	next := officeInstalledPackages()
	changed := false
	if len(next) != len(a.installed) {
		changed = true
	} else {
		for k := range next {
			if !a.installed[k] {
				changed = true
				break
			}
		}
	}
	a.installed = next
	if changed {
		for _, c := range officeComponents {
			if officeCompInstalled(c, a.installed) {
				a.sel[c.ID] = true
			}
		}
	}
	return changed
}

func (a *OfficeSetupApp) recommend() {
	a.sel = officeSelectionDefaults(a.installed)
	a.d.Invalidate()
}

func (a *OfficeSetupApp) selectAll() {
	for _, c := range officeComponents {
		a.sel[c.ID] = true
	}
	a.d.Invalidate()
}

func (a *OfficeSetupApp) toggle(id string) {
	c := officeCompByID(id)
	if c == nil || c.Required || officeCompInstalled(*c, a.installed) {
		return
	}
	a.sel[id] = !a.sel[id]
	a.d.Invalidate()
}

func installStatusRunning() bool {
	data, err := os.ReadFile(installStatusPath)
	if err != nil {
		return false
	}
	_, _, _, state := parseInstallStatus(string(data))
	if state != "running" {
		return false
	}
	if st, err := os.Stat(installStatusPath); err == nil {
		return time.Since(st.ModTime()) < 20*time.Minute
	}
	return true
}

func officePIDAlive() bool {
	data, err := os.ReadFile(officeSetupPIDPath)
	if err != nil {
		return false
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return false
	}
	return processAlive(pid)
}

func (a *OfficeSetupApp) installSelected() {
	if a.installing || a.d == nil {
		return
	}
	compNames := make([]string, 0, 6)
	for _, c := range officeComponents {
		if a.sel[c.ID] && !officeCompInstalled(c, a.installed) {
			compNames = append(compNames, c.Name)
		}
	}
	pkgs := officeMissingPackages(a.sel, a.installed)
	if len(pkgs) == 0 {
		a.d.MessageBox("Office 365 全件套", "所选组件已经全部安装，不需要下载。\n\n可以继续勾选其他组件进行补装。", IconInfo, []string{"确定"}, nil)
		return
	}
	if installStatusRunning() {
		a.d.MessageBox("Office 365 全件套",
			"另一个安装任务正在运行。\n\n可以打开“安装进度”窗口查看，等它完成后再安装 Office 组件。",
			IconWarning, []string{"确定"}, func(int) { a.d.openInstallStatus("安装进度") })
		return
	}
	mb := officeSelectedDownloadMB(a.sel, a.installed)
	msg := fmt.Sprintf("将安装 %d 个组件：\n\n· %s\n\n需要下载约 %d MB 软件包（安装后占用更多磁盘空间）。\n首次安装完整套件可能需要 10–30 分钟，取决于网络速度。\n\n安装期间可以继续使用桌面，进度显示在“安装进度”窗口中。",
		len(compNames), strings.Join(compNames, "\n· "), mb)
	a.d.MessageBox("确认安装 Office 组件", msg, IconQuestion, []string{"开始安装", "取消"}, func(i int) {
		if i == 0 {
			a.startInstaller(pkgs)
		}
	})
}

func (a *OfficeSetupApp) startInstaller(pkgs []string) {
	if len(pkgs) == 0 {
		return
	}
	if _, err := os.Stat("/usr/bin/win95-office-setup"); err != nil {
		a.d.MessageBox("Office 365 全件套", "镜像缺少 win95-office-setup 安装脚本，无法继续。", IconError, []string{"确定"}, nil)
		return
	}
	_ = os.Remove(installStatusPath)
	cmd := exec.Command("/usr/bin/win95-office-setup", pkgs...)
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
	}
	if err := cmd.Start(); err != nil {
		a.d.MessageBox("Office 365 全件套", "无法启动安装任务：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	_ = cmd.Process.Release()
	a.installing = true
	a.installStarted = time.Now()
	a.installMsg = "正在准备安装…"
	a.msg = ""
	a.d.openInstallStatus("Office 365 全件套")
	a.d.Invalidate()
}

func (a *OfficeSetupApp) pollInstall() {
	if !a.installing {
		return
	}
	data, err := os.ReadFile(installStatusPath)
	if err == nil {
		_, _, msg, state := parseInstallStatus(string(data))
		if msg != "" {
			a.installMsg = msg
		}
		if state == "done" || state == "failed" {
			if st, statErr := os.Stat(installStatusPath); statErr == nil && st.ModTime().Before(a.installStarted.Add(-2*time.Second)) {
				// Stale status from an earlier install; ignore it.
			} else {
				a.finishInstall(state == "done", msg)
				return
			}
		}
	}
	if time.Since(a.installStarted) > 8*time.Second && !officePIDAlive() {
		a.finishInstall(false, "安装进程已退出，但没有收到完成状态。请打开安装进度窗口查看日志。")
	}
}

func (a *OfficeSetupApp) finishInstall(ok bool, msg string) {
	a.installing = false
	a.installMsg = ""
	changed := a.refreshInstalled()
	if changed {
		// Keep installed components ticked; leave optional picks as-is.
		a.sel = officeSelectionDefaults(a.installed)
		if m := officeModuleByID(a.pendingModule); m != nil {
			a.sel[m.Comp] = true
		}
	}
	if !ok {
		if msg == "" {
			msg = "未知错误"
		}
		a.d.MessageBox("Office 组件安装失败", msg+"\n\n详情请查看安装进度窗口中的日志。", IconError, []string{"确定"}, nil)
		a.d.Invalidate()
		return
	}
	beep()
	a.msg = "安装完成。已安装的组件会显示为“已安装”，以后可以随时补装其他组件。"
	a.d.Invalidate()
	mod := a.pendingModule
	path := a.pendingPath
	a.pendingModule = ""
	a.pendingPath = ""
	a.d.MessageBox("Office 365 全件套", "所选组件已安装完成。\n\n现在可以打开 Office 启动器使用这些应用。",
		IconInfo, []string{"确定"}, func(int) {
			if mod != "" {
				a.d.openOfficeModule(mod, path)
			}
		})
}

func (a *OfficeSetupApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if now.Sub(a.lastRefresh) > 2*time.Second {
		a.lastRefresh = now
		if a.refreshInstalled() {
			d.Invalidate()
		}
	}
	if a.installing && now.Sub(a.lastPoll) > 700*time.Millisecond {
		a.lastPoll = now
		a.pollInstall()
		d.Invalidate()
	}
}

func (a *OfficeSetupApp) layout(r ui.Rect) {
	btnY := r.Bottom() - 38
	a.btnClose = ui.Rect{r.Right() - 14 - 80, btnY, 80, 28}
	a.btnInst = ui.Rect{a.btnClose.X - 8 - 112, btnY, 112, 28}
	a.btnAll = ui.Rect{a.btnInst.X - 8 - 84, btnY, 84, 28}
	a.btnRec = ui.Rect{a.btnAll.X - 8 - 96, btnY, 96, 28}
	a.btnProg = ui.Rect{a.btnInst.X - 8 - 96, btnY, 96, 28}
	a.listR = ui.Rect{r.X + 12, r.Y + 128, r.W - 24, r.H - 128 - 52}
	if a.listR.H < 60 {
		a.listR.H = 60
	}
	a.scroll.Visible = a.listR.H / officeRowH
	if a.scroll.Visible < 1 {
		a.scroll.Visible = 1
	}
	a.scroll.Total = len(officeComponents)
	if a.reveal != "" {
		for i, cc := range officeComponents {
			if cc.ID == a.reveal {
				a.scroll.Pos = i - a.scroll.Visible/2
				break
			}
		}
		a.reveal = ""
	}
	a.scroll.Clamp()
	a.rows = a.rows[:0]
	for i := 0; i < a.scroll.Visible; i++ {
		a.rows = append(a.rows, ui.Rect{a.listR.X + 1, a.listR.Y + 1 + i*officeRowH, a.listR.W - 18, officeRowH})
	}
}

func (a *OfficeSetupApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	a.layout(r)
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	header := ui.Rect{r.X, r.Y, r.W, 62}
	c.TitleBar(header, 0x1F3864, 0x2E75B6)
	f.TextBold(c, header.X+18, header.Y+12, "Office 365 全件套", ui.ColWhite)
	f.Text(c, header.X+18, header.Y+36, "按需选择组件；已安装的组件会自动识别，可随时回来补装。", 0xC8DCFF)
	installedN := 0
	for _, cc := range officeComponents {
		if officeCompInstalled(cc, a.installed) {
			installedN++
		}
	}
	f.TextRight(c, header.Right()-18, header.Y+12, fmt.Sprintf("已安装 %d / %d 个组件", installedN, len(officeComponents)), 0xC8DCFF)

	line := a.msg
	if a.installing {
		line = "正在安装： " + a.installMsg + "　（可继续使用桌面，进度见“安装进度”窗口）"
	}
	if line == "" {
		line = "勾选要安装的组件，然后单击“安装所选”。核心组件为必装项。"
	}
	f.TextClip(c, r.X+16, r.Y+72, r.W-32, line, ui.ColText)
	f.TextClip(c, r.X+16, r.Y+94, r.W-32, "文档将通过内嵌 X11 窗口打开，支持 fcitx5 中文输入法。", ui.ColShadow)

	a.scroll.Total = len(officeComponents)
	a.scroll.Clamp()
	c.FillRect(a.listR, ui.ColWhite)
	c.Sunken(a.listR)

	for i := 0; i < a.scroll.Visible; i++ {
		idx := a.scroll.Pos + i
		if idx >= len(officeComponents) {
			break
		}
		comp := officeComponents[idx]
		row := a.rows[i]
		installed := officeCompInstalled(comp, a.installed)
		checked := a.sel[comp.ID]
		hover := a.press == "" && row.Contains(d.MouseX, d.MouseY) && !installed && !comp.Required
		if hover {
			c.FillRect(row, 0xE8F0FA)
		}
		cbEnabled := !installed && !comp.Required && !a.installing
		ui.Checkbox(c, row.X+8, row.Y+8, checked, cbEnabled)
		title := comp.Name
		if comp.Required {
			title += "（必装）"
		}
		f.TextBold(c, row.X+28, row.Y+4, title, ui.ColText)
		desc := comp.Desc
		status := fmt.Sprintf("约 %d MB", comp.SizeMB)
		statusCol := ui.ColShadow
		if installed {
			status = "已安装"
			statusCol = 0x1B7A2E
			desc += "　"
			c.FillRect(ui.Rect{row.X + 3, row.Y + 2, 3, row.H - 5}, 0x2E8B57)
		} else if comp.Required {
			status = "核心"
			statusCol = ui.ColNavy
		}
		f.TextClip(c, row.X+28, row.Y+23, row.W-28-96, desc, ui.ColShadow)
		f.TextRight(c, row.Right()-8, row.Y+24, status, statusCol)
		if i < a.scroll.Visible-1 {
			c.HLine(row.X, row.Bottom()-1, row.W, 0xE4E4E4)
		}
	}
	scrollR := ui.Rect{a.listR.Right() - 15, a.listR.Y + 1, 14, a.listR.H - 2}
	a.scroll.Draw(c, scrollR)

	selMB := officeSelectedDownloadMB(a.sel, a.installed)
	missingN := officeMissingCount(a.sel, a.installed)
	summary := fmt.Sprintf("已选 %d 个待安装组件，预计下载约 %d MB", missingN, selMB)
	if missingN == 0 {
		summary = "所选组件均已安装"
	}
	if a.installing {
		summary = "正在安装，请稍候…"
	}
	f.Text(c, r.X+16, r.Bottom()-30, summary, ui.ColText)

	ui.Button(c, a.btnRec, "推荐选择", false, !a.installing)
	ui.Button(c, a.btnAll, "全选", false, !a.installing)
	if a.installing {
		ui.Button(c, a.btnProg, "查看进度", false, true)
		ui.Button(c, a.btnInst, "正在安装…", false, false)
	} else {
		ui.Button(c, a.btnInst, "安装所选", false, missingN > 0)
	}
	ui.Button(c, a.btnClose, "关闭", false, true)
}

func (a *OfficeSetupApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	a.layout(client)
	if ev.Kind == MouseWheel {
		if a.listR.Contains(ev.X, ev.Y) {
			a.scroll.ScrollBy(-ev.Wheel)
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
		switch {
		case a.btnRec.Contains(ev.X, ev.Y):
			a.press = "rec"
		case a.btnAll.Contains(ev.X, ev.Y):
			a.press = "all"
		case a.btnInst.Contains(ev.X, ev.Y):
			a.press = "install"
		case a.btnProg.Contains(ev.X, ev.Y):
			a.press = "progress"
		case a.btnClose.Contains(ev.X, ev.Y):
			a.press = "close"
		default:
			scrollR := ui.Rect{a.listR.Right() - 15, a.listR.Y + 1, 14, a.listR.H - 2}
			if scrollR.Contains(ev.X, ev.Y) {
				if changed, _ := a.scroll.Mouse(ev, scrollR); changed {
					d.Invalidate()
				}
				a.press = "scroll"
			} else if a.listR.Contains(ev.X, ev.Y) {
				rowIdx := (ev.Y - a.listR.Y - 1) / officeRowH
				if rowIdx >= 0 && rowIdx < len(a.rows) {
					a.press = fmt.Sprintf("row:%d", a.scroll.Pos+rowIdx)
				}
			}
		}
		if a.press != "" {
			d.Invalidate()
		}
	case MouseUp:
		p := a.press
		a.press = ""
		if strings.HasPrefix(p, "row:") {
			var idx int
			if _, err := fmt.Sscanf(p, "row:%d", &idx); err == nil && idx >= 0 && idx < len(officeComponents) {
				a.toggle(officeComponents[idx].ID)
			}
			return
		}
		switch p {
		case "rec":
			if a.btnRec.Contains(ev.X, ev.Y) {
				a.recommend()
			}
		case "all":
			if a.btnAll.Contains(ev.X, ev.Y) {
				a.selectAll()
			}
		case "install":
			if a.btnInst.Contains(ev.X, ev.Y) {
				a.installSelected()
			}
		case "progress":
			if a.btnProg.Contains(ev.X, ev.Y) {
				d.openInstallStatus("Office 365 全件套")
			}
		case "close":
			if a.btnClose.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
		d.Invalidate()
	}
}

func (a *OfficeSetupApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl || ev.Alt {
		return false
	}
	switch ev.Name {
	case "Esc":
		if !a.installing {
			d.CloseWindow(w)
		}
		return true
	case "F5":
		if a.refreshInstalled() {
			d.Invalidate()
		}
		return true
	case "Enter":
		if !a.installing {
			a.installSelected()
		}
		return true
	}
	return false
}

// openOfficeSetup opens (or reuses) the component picker and optionally preselects
// the component needed by a document the user just double-clicked.
func (d *Desktop) openOfficeSetup(comp, module, path string) *OfficeSetupApp {
	for _, w := range d.windows {
		if app, ok := w.App.(*OfficeSetupApp); ok {
			d.bringToFront(w)
			if !app.installing && comp != "" {
				app.sel[comp] = true
				app.reveal = comp
				if c := officeCompByID(comp); c != nil {
					app.msg = "要先安装“" + c.Name + "”才能完成刚才的操作。"
				}
			}
			if !app.installing {
				app.pendingModule = module
				app.pendingPath = path
			}
			app.refreshInstalled()
			d.Invalidate()
			return app
		}
	}
	app := NewOfficeSetupApp(d, comp, module, path)
	w := d.Open(app)
	app.win = w
	return app
}
