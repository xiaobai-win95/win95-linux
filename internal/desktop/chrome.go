package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

func (d *Desktop) Draw(c *ui.Canvas) {
	if d.bsod {
		d.drawBSOD(c)
		return
	}
	if d.sleepPending || d.sleeping {
		d.drawSleep(c)
		return
	}
	if d.saver {
		d.drawScreensaver(c)
		return
	}
	c.DesktopPattern(ui.Rect{0, 0, c.W, c.H})
	d.drawIcons(c)
	for _, w := range d.windows {
		if w.Minimized {
			continue
		}
		d.refreshMenus(w)
		d.drawWindow(c, w)
	}
	d.drawTaskbar(c)
	if d.startOpen && d.modal == nil {
		d.drawStartMenu(c)
	}
	if d.modal == nil && d.active != nil && d.active.menuOpen >= 0 && !d.active.Minimized {
		d.refreshMenus(d.active)
		d.drawMenuDropdown(c, d.active)
	}
	if d.modal == nil {
		d.drawContext(c)
	}
	if d.hand {
		ui.CursorHand(c, d.MouseX, d.MouseY)
	} else {
		ui.Cursor(c, d.MouseX, d.MouseY)
	}
}

func (d *Desktop) titleButtons(w *Window) (minR, maxR, closeR ui.Rect) {
	t := w.TitleRect()
	closeR = ui.Rect{X: t.Right() - 23, Y: t.Y + 3, W: btnW, H: btnH}
	maxR = ui.Rect{X: closeR.X - 22, Y: t.Y + 3, W: btnW, H: btnH}
	minR = ui.Rect{X: maxR.X - 22, Y: t.Y + 3, W: btnW, H: btnH}
	return
}

func (d *Desktop) drawWindow(c *ui.Canvas, w *Window) {
	outer := w.Outer()
	c.FillRect(outer, ui.ColFace)
	c.Raised(outer)

	title := w.TitleRect()
	active := w == d.active
	if active {
		c.TitleBar(title, ui.ColNavy, ui.ColNavyLight)
	} else {
		c.TitleBar(title, ui.ColTitleInac, 0xB0B0B0)
	}
	ui.DrawIcon16(c, w.App.Icon(), title.X+4, title.Y+4)
	f := ui.DefaultFont()
	tcol := uint32(ui.ColWhite)
	if !active {
		tcol = ui.ColLight
	}
	textW := title.W - 3*22 - 30
	f.TextClip(c, title.X+24, title.Y+4, textW, w.App.Title(), tcol)

	minR, maxR, closeR := d.titleButtons(w)
	ui.SmallButton(c, minR, "_", w.pressedBtn == "min")
	if w.Resizable {
		ui.SmallButton(c, maxR, "□", w.pressedBtn == "max")
	} else {
		ui.Button(c, maxR, "", false, false)
	}
	ui.SmallButton(c, closeR, "×", w.pressedBtn == "close")

	if len(w.menus) > 0 {
		mb := w.MenuBarRect()
		c.FillRect(mb, ui.ColFace)
		c.HLine(mb.X, mb.Bottom()-1, mb.W, ui.ColShadow)
		c.HLine(mb.X, mb.Bottom()-2, mb.W, ui.ColWhite)
		x := mb.X + 4
		for i, m := range w.menus {
			lw := f.Width(m.Label) + 14
			lr := ui.Rect{x, mb.Y + 1, lw, mb.H - 3}
			if w.menuOpen == i {
				c.FillRect(lr, ui.ColFace)
				c.Sunken(lr)
			}
			f.Text(c, lr.X+7, lr.Y+3, m.Label, ui.ColText)
			x += lw
		}
	} else if w.menuOpen >= 0 {
		w.menuOpen = -1
	}

	client := w.ClientRect()
	if client.W > 0 && client.H > 0 {
		w.App.Paint(d, w, c, client)
	}

	if w.Resizable && !w.Maximized {
		d.drawGrip(c, w)
	}
}

func (d *Desktop) drawGrip(c *ui.Canvas, w *Window) {
	x := w.X + w.W - 15
	y := w.Y + w.H - 15
	for i := 0; i < 4; i++ {
		c.HLine(x+i*3, y+11, 2, ui.ColWhite)
		c.HLine(x+i*3, y+12, 2, ui.ColShadow)
		c.HLine(x+11, y+i*3, 2, ui.ColWhite)
		c.HLine(x+12, y+i*3, 2, ui.ColShadow)
	}
}

func (d *Desktop) drawTaskbar(c *ui.Canvas) {
	top := d.taskbarTop()
	c.FillRect(ui.Rect{0, top, d.W, taskbarH}, ui.ColFace)
	c.HLine(0, top, d.W, ui.ColWhite)
	c.HLine(0, top+1, d.W, ui.ColLight)
	c.HLine(0, top+2, d.W, ui.ColShadow)

	start := ui.Rect{4, top + 4, startBtnW - 2, 26}
	ui.Button(c, start, "", d.startOpen, true)
	ui.FlagLogo(c, start.X+7, start.Y+5, 16)
	f := ui.DefaultFont()
	f.TextBold(c, start.X+28, start.Y+5, "开始", ui.ColText)

	clock := d.clockRect()
	for _, tb := range d.taskbarButtons() {
		pressed := tb.w == d.active && !tb.w.Minimized
		ui.Button(c, tb.r, "", pressed, true)
		ui.DrawIcon16(c, tb.w.App.Icon(), tb.r.X+4, tb.r.Y+5)
		f.TextClip(c, tb.r.X+24, tb.r.Y+5, tb.r.W-28, tb.w.App.Title(), ui.ColText)
	}

	d.drawSysTray(c)
	c.FillRect(clock, ui.ColFace)
	c.Sunken(clock)
	f.Text(c, clock.X+(clock.W-f.Width(d.clock))/2, clock.Y+4, d.clock, ui.ColText)
}

func (d *Desktop) drawIcons(c *ui.Canvas) {
	f := ui.DefaultFont()
	for i, ic := range d.icons {
		x, y := d.iconPos(i)
		selected := i == d.selected
		ui.DrawIcon32(c, ic.kind, x+24, y+2, selected)
		cx := x + 40
		label := ic.label
		tw := f.Width(label)
		lx := cx - tw/2
		ly := y + 40
		if selected {
			c.FillRect(ui.Rect{lx - 2, ly - 1, tw + 4, 18}, ui.ColNavy)
			f.Text(c, lx, ly, label, ui.ColWhite)
		} else {
			f.Text(c, lx+1, ly+1, label, ui.ColDark)
			f.Text(c, lx, ly, label, ui.ColWhite)
		}
	}
}

func (d *Desktop) iconPos(i int) (int, int) {
	if i < 0 || i >= len(d.icons) {
		return 16, 14
	}
	if !d.autoArrange {
		if p, ok := d.iconPositions[d.icons[i].key]; ok {
			return p.x, p.y
		}
		x, y := d.gridIconPos(i)
		for tries := 0; tries < 512; tries++ {
			taken := false
			for j, ic := range d.icons {
				if j == i {
					continue
				}
				if p, ok := d.iconPositions[ic.key]; ok && p.x == x && p.y == y {
					taken = true
					break
				}
			}
			if !taken {
				return x, y
			}
			y += 84
			if y+76 > d.taskbarTop() {
				x += 94
				y = 14
			}
			if x+86 > d.W {
				return d.gridIconPos(i)
			}
		}
		return d.gridIconPos(i)
	}
	return d.gridIconPos(i)
}

func (d *Desktop) iconAt(x, y int) int {
	for i := range d.icons {
		ix, iy := d.iconPos(i)
		if (ui.Rect{ix, iy, 86, 76}).Contains(x, y) {
			return i
		}
	}
	return -1
}

// startEntry is one row in the start menu or a submenu.
type startEntry struct {
	id       string
	label    string
	icon     ui.IconKind
	action   func()
	sep      bool
	enabled  bool
	children []startEntry
}

func (d *Desktop) menuEntries() []startEntry {
	entries := []startEntry{
		{id: "prog", label: "程序(P)", icon: ui.IcoFolderOpen, enabled: true, children: d.programEntries()},
		{id: "docs", label: "文档(D)", icon: ui.IcoFolder, enabled: true, children: d.documentEntries()},
		{id: "settings", label: "设置(S)", icon: ui.IcoSetup, enabled: true, children: d.settingsEntries()},
		{id: "find", label: "查找(F)", icon: ui.IcoFolderOpen, enabled: true, action: func() { d.openFiles("") }},
		{id: "help", label: "帮助(H)", icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
		{sep: true},
	}
	if d.Mode == ModeDesktop {
		entries = append(entries, startEntry{id: "sleep", label: "睡眠(S)…", icon: ui.IcoComputer, enabled: true, action: func() { d.showSleep() }})
	}
	entries = append(entries,
		startEntry{id: "logoff", label: "注销(L)…", icon: ui.IcoComputer, enabled: true, action: func() { d.showLogoff() }},
		startEntry{id: "restart", label: "重新启动(R)…", icon: ui.IcoSetup, enabled: true, action: func() { d.showRestart() }},
		startEntry{id: "shutdown", label: "关闭计算机(U)…", icon: ui.IcoComputer, enabled: true, action: func() { d.showShutdown() }},
	)
	return entries
}

func (d *Desktop) startMenuHeight() int {
	h := 8
	for _, e := range d.menuEntries() {
		if e.sep {
			h += 9
		} else {
			h += 27
		}
	}
	return h
}

func (d *Desktop) startMenuRect() ui.Rect {
	h := d.startMenuHeight()
	return ui.Rect{4, d.taskbarTop() - h - 2, startMenuW, h}
}

func (d *Desktop) startItemRects() []ui.Rect {
	r := d.startMenuRect()
	var out []ui.Rect
	y := r.Y + 6
	for _, e := range d.menuEntries() {
		if e.sep {
			y += 9
			out = append(out, ui.Rect{})
			continue
		}
		out = append(out, ui.Rect{r.X + 30, y, r.W - 34, 26})
		y += 27
	}
	return out
}

func (d *Desktop) drawStartMenu(c *ui.Canvas) {
	r := d.startMenuRect()
	c.FillRect(r, ui.ColFace)
	c.Raised(r)

	banner := ui.Rect{r.X + 3, r.Y + 3, 26, r.H - 6}
	c.TitleBar(banner, 0x000060, 0x000080)
	f := ui.DefaultFont()
	label := "Windows 95"
	xb := banner.X + 7
	yb := banner.Y + 8
	for _, ch := range label {
		f.Text(c, xb, yb, string(ch), 0xFFFFFF)
		yb += 15
	}
	f.Text(c, banner.X+7, banner.Bottom()-30, "Linux", 0xC0E0FF)

	entries := d.menuEntries()
	rects := d.startItemRects()
	y := r.Y + 6
	for i, e := range entries {
		if e.sep {
			c.HLine(r.X+34, y+4, r.W-40, ui.ColShadow)
			c.HLine(r.X+34, y+5, r.W-40, ui.ColWhite)
			y += 9
			continue
		}
		ir := rects[i]
		hover := d.startSel == e.id
		if hover && e.enabled {
			c.FillRect(ir, ui.ColNavy)
		}
		ui.DrawIcon16(c, e.icon, ir.X+4, ir.Y+5)
		col := ui.ColText
		if !e.enabled {
			col = ui.ColTextDis
		}
		if hover && e.enabled {
			col = ui.ColWhite
		}
		f.Text(c, ir.X+26, ir.Y+5, e.label, col)
		if len(e.children) > 0 {
			ac := ui.ColText
			if hover {
				ac = ui.ColWhite
			}
			ui.Arrow(c, ir.Right()-12, ir.Y+11, "right", ac)
		}
		y += 27
	}

	if d.startSub != "" {
		for i, e := range entries {
			if e.id != d.startSub || len(e.children) == 0 {
				continue
			}
			sr := d.startSubRect(rects[i], e)
			c.FillRect(sr, ui.ColFace)
			c.Raised(sr)
			yy := sr.Y + 4
			for _, sub := range e.children {
				if sub.sep {
					c.HLine(sr.X+8, yy+4, sr.W-16, ui.ColShadow)
					c.HLine(sr.X+8, yy+5, sr.W-16, ui.ColWhite)
					yy += 9
					continue
				}
				rr := ui.Rect{sr.X + 4, yy, sr.W - 8, 25}
				hov := d.startHover == sub.id
				if hov && sub.enabled {
					c.FillRect(rr, ui.ColNavy)
				}
				if sub.icon != ui.IconNone {
					ui.DrawIcon16(c, sub.icon, rr.X+5, rr.Y+5)
				}
				col := ui.ColText
				if !sub.enabled {
					col = ui.ColTextDis
				}
				if hov && sub.enabled {
					col = ui.ColWhite
				}
				f.Text(c, rr.X+28, rr.Y+5, sub.label, col)
				yy += 26
			}
		}
	}
}

func (d *Desktop) startSubRect(anchor ui.Rect, e startEntry) ui.Rect {
	h := 8
	for _, s := range e.children {
		if s.sep {
			h += 9
		} else {
			h += 26
		}
	}
	x := d.startMenuRect().Right() - 4
	w := 226
	if x+w > d.W-4 {
		x = d.startMenuRect().X + 4 - w
	}
	// The 程序 submenu is now taller than the space below its anchor row;
	// shift it up so the last entries stay above the taskbar.
	y := anchor.Y - 2
	if y+h > d.taskbarTop()-2 {
		y = d.taskbarTop() - 2 - h
	}
	if y < 2 {
		y = 2
	}
	return ui.Rect{x, y, w, h}
}

func (d *Desktop) startSubAt(x, y int) (string, string) {
	entries := d.menuEntries()
	rects := d.startItemRects()
	for i, e := range entries {
		if e.id != d.startSub || len(e.children) == 0 {
			continue
		}
		sr := d.startSubRect(rects[i], e)
		if !sr.Contains(x, y) {
			continue
		}
		yy := sr.Y + 4
		for _, sub := range e.children {
			if sub.sep {
				yy += 9
				continue
			}
			if (ui.Rect{sr.X + 4, yy, sr.W - 8, 25}).Contains(x, y) {
				return e.id, sub.id
			}
			yy += 26
		}
	}
	return "", ""
}

func (d *Desktop) startItemAt(x, y int) string {
	rects := d.startItemRects()
	for i, e := range d.menuEntries() {
		if e.sep {
			continue
		}
		if rects[i].Contains(x, y) {
			return e.id
		}
	}
	return ""
}

func (d *Desktop) subEntry(id string) *startEntry {
	for _, e := range d.menuEntries() {
		for i := range e.children {
			if e.children[i].id == id {
				return &e.children[i]
			}
		}
	}
	return nil
}

func (d *Desktop) showAbout() {
	d.MessageBox("关于 Windows 95 Linux",
		"Windows 95 Linux 中文版\n版本 1.0 · ARM64 / UEFI\n\nWin95 风格图形环境，内置终端、记事本、计算器、日历、文件管理器与网络浏览器。\n\n"+d.Host.Info,
		IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) showShutdown() {
	msg := "你确定要关闭计算机吗？\n\n未保存的数据将会丢失。"
	if d.Mode == ModeInstaller {
		msg = "安装尚未完成，关闭计算机会中止安装。\n\n仍要关闭计算机吗？"
	}
	d.MessageBox("关闭 Windows", msg, IconQuestion, []string{"确定", "取消"}, func(i int) {
		if i != 0 {
			return
		}
		if d.Host.PowerOff != nil {
			d.Host.PowerOff()
		} else {
			d.MessageBox("提示", "预览模式无法关机。", IconInfo, []string{"确定"}, nil)
		}
	})
}

func (d *Desktop) showRestart() {
	msg := "你确定要重新启动计算机吗？\n\n未保存的数据将会丢失。"
	if d.Mode == ModeInstaller {
		msg = "安装尚未完成，重新启动会中止安装。\n\n仍要重新启动吗？"
	}
	d.MessageBox("重新启动 Windows", msg, IconQuestion, []string{"确定", "取消"}, func(i int) {
		if i != 0 {
			return
		}
		if d.Host.Reboot != nil {
			d.Host.Reboot()
		} else {
			d.MessageBox("提示", "预览模式无法重新启动。", IconInfo, []string{"确定"}, nil)
		}
	})
}

func (d *Desktop) showLogoff() {
	if d.Mode == ModeInstaller {
		d.MessageBox("注销", "安装程序运行期间无法注销。\n\n请先完成或关闭安装程序。", IconInfo, []string{"确定"}, nil)
		return
	}
	d.MessageBox("注销 Windows", "你确定要注销当前用户吗？\n\n所有程序都会关闭，未保存的数据将会丢失。", IconQuestion,
		[]string{"确定", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if d.Host.Logout != nil {
				d.Host.Logout()
			} else {
				d.MessageBox("提示", "预览模式无法注销。", IconInfo, []string{"确定"}, nil)
			}
		})
}

func (d *Desktop) showDangerMenu() {
	d.MessageBox("危险操作", "这个按钮会模拟一次 Windows 95 蓝屏死机。\n\n屏幕会进入全屏蓝色报错界面，按任意键（约 1 秒后）即可返回桌面。\n\n这不会损坏系统或文件。", IconWarning,
		[]string{"继续", "取消"}, func(i int) {
			if i == 0 {
				d.showBSOD()
			}
		})
}

func (d *Desktop) programEntries() []startEntry {
	items := []startEntry{
		{id: "notepad", label: "记事本", icon: ui.IcoNotepad, enabled: true, action: func() { d.openNotepad("") }},
		{id: "calc", label: "计算器", icon: ui.IcoCalc, enabled: true, action: func() { d.openCalc() }},
		{id: "calendar", label: "日历", icon: ui.IcoCalendar, enabled: true, action: func() { d.openCalendar() }},
		{id: "term", label: "终端", icon: ui.IcoTerminal, enabled: true, action: func() { d.openTerminal() }},
		{id: "files", label: "文件管理器", icon: ui.IcoFolder, enabled: true, action: func() { d.openFiles("/root") }},
		{id: "browser", label: "网络浏览器", icon: ui.IcoBrowser, enabled: true, action: func() { d.openBrowser("") }},
		{sep: true},
		{id: "pkgman", label: "软件包管理器", icon: ui.IcoPackage, enabled: true, action: func() { d.openPkgMan() }},
		{id: "mines", label: "扫雷", icon: ui.IcoMine, enabled: true, action: func() { d.openMines() }},
		{id: "paint", label: "画图", icon: ui.IcoPaint, enabled: true, action: func() { d.openPaint() }},
		{sep: true},
		{id: "sysinfo", label: "系统信息", icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
	}
	if d.Mode == ModeInstaller {
		items = []startEntry{
			{id: "setup", label: "安装向导", icon: ui.IcoSetup, enabled: true, action: func() { d.focusInstaller() }},
			{id: "term", label: "终端", icon: ui.IcoTerminal, enabled: true, action: func() { d.openTerminal() }},
		}
	}
	return items
}

func (d *Desktop) documentEntries() []startEntry {
	if d.Mode == ModeInstaller {
		return []startEntry{
			{id: "readme", label: "安装说明.txt", icon: ui.IcoText, enabled: true, action: func() { d.showInstallHelp() }},
		}
	}
	return []startEntry{
		{id: "welcome", label: "欢迎使用.txt", icon: ui.IcoText, enabled: true, action: func() { d.openNotepad("/root/文档/欢迎使用.txt") }},
		{id: "plan", label: "工作计划.txt", icon: ui.IcoText, enabled: true, action: func() { d.openNotepad("/root/文档/工作计划.txt") }},
		{id: "meeting", label: "会议记录.txt", icon: ui.IcoText, enabled: true, action: func() { d.openNotepad("/root/文档/会议记录.txt") }},
		{sep: true},
		{id: "docsdir", label: "我的文档文件夹", icon: ui.IcoFolder, enabled: true, action: func() { d.openFiles("/root/文档") }},
		{id: "dl", label: "下载文件夹", icon: ui.IcoDownload, enabled: true, action: func() { d.openFiles("/root/下载") }},
	}
}

func (d *Desktop) settingsEntries() []startEntry {
	return []startEntry{
		{id: "about", label: "关于本机", icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
		{id: "pkgman", label: "添加/删除软件", icon: ui.IcoPackage, enabled: true, action: func() { d.openPkgMan() }},
		{id: "files", label: "文件管理器", icon: ui.IcoFolder, enabled: true, action: func() { d.openFiles("/root") }},
		{sep: true},
		{id: "xfce", label: "切换到 XFCE 桌面", icon: ui.IcoComputer, enabled: true, action: func() { d.switchToXFCE() }},
		{id: "danger", label: "危险操作…", icon: ui.IcoInfo, enabled: true, action: func() { d.showDangerMenu() }},
	}
}

func (d *Desktop) focusInstaller() {
	if d.installer != nil && d.installer.win != nil {
		d.bringToFront(d.installer.win)
	}
}

func (d *Desktop) showInstallHelp() {
	d.MessageBox("安装说明",
		"1. 选择一块目标磁盘（通常是第二块 VirtIO 磁盘）。\n2. 安装程序会重新分区并清空该磁盘。\n3. 完成后关闭虚拟机，移除安装盘，再从目标磁盘启动。\n\n安装盘本身会标记为“安装盘”，不会被误选。",
		IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) statusClock() string {
	return fmt.Sprintf("%s %s", time.Now().In(d.clockLoc).Format("2006-01-02"), d.clock)
}
