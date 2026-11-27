package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

func (d *Desktop) Draw(c *ui.Canvas) {
	if d.prank != nil {
		d.prank.draw(d, c)
		return
	}
	d.drawScene(c, true)
}

// drawScene renders the normal desktop without the prank overlay. The prank
// takes a cursor-free snapshot of it for its full-screen transformations.
func (d *Desktop) drawScene(c *ui.Canvas, withCursor bool) {
	if d.bsod {
		d.drawBSOD(c)
		return
	}
	if d.nuke {
		d.drawNuke(c)
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
	d.drawDesktopBackground(c)
	d.drawIcons(c)
	for _, w := range d.windows {
		if w.Minimized || w.Workspace != d.ws {
			continue
		}
		d.refreshMenus(w)
		d.drawWindow(c, w)
	}
	d.drawTaskbar(c)
	d.drawWorkspaceToast(c)
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
	d.drawNotifications(c)
	d.drawIME(c)
	if d.spotlight != nil && d.spotlight.active {
		d.spotlight.Paint(c)
	}
	d.drawFileDrag(c)
	d.drawSoftKeyboard(c)
	if withCursor {
		if d.hand {
			ui.CursorHand(c, d.MouseX, d.MouseY)
		} else if d.resizeCursor != "" {
			ui.CursorResize(c, d.MouseX, d.MouseY, d.resizeCursor)
		} else {
			ui.Cursor(c, d.MouseX, d.MouseY)
		}
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
	f.TextClip(c, title.X+24, title.Y+4, textW, d.tr(w.App.Title()), tcol)

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
	f.TextBold(c, start.X+28, start.Y+5, d.tr("开始"), ui.ColText)

	clock := d.clockRect()
	for _, tb := range d.taskbarButtons() {
		pressed := tb.w == d.active && !tb.w.Minimized
		ui.Button(c, tb.r, "", pressed, true)
		ui.DrawIcon16(c, tb.w.App.Icon(), tb.r.X+4, tb.r.Y+5)
		f.TextClip(c, tb.r.X+24, tb.r.Y+5, tb.r.W-28, d.tr(tb.w.App.Title()), ui.ColText)
	}

	d.drawSysTray(c)
	d.drawIMETray(c)
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
	danger   bool
	children []startEntry
}

func (d *Desktop) menuEntries() []startEntry {
	entries := []startEntry{
		{id: "aoltrial", label: "AOL & Internet 免费试用", icon: ui.IcoOnline, enabled: true, action: func() { d.openAOL(0) }},
		{id: "aol30", label: "AOL 3.0i（含 Internet Explorer 4.0）", icon: ui.IcoIE, enabled: true, action: func() { d.openAOL(1) }},
		{id: "aolca", label: "AOL Canada 3.0i（含 IE 4.0）", icon: ui.IcoIE, enabled: true, action: func() { d.openAOL(2) }},
		{sep: true},
		{id: "newoffice", label: d.tr("新建 Office 文档(N)"), icon: ui.IcoOffice, enabled: true, action: func() { d.openOffice() }},
		{id: "openoffice", label: d.tr("打开 Office 文档(O)…"), icon: ui.IcoOffice, enabled: true, action: func() {
			d.FileDialog(FileOpen, "/root/文档", "", func(path string, ok bool) {
				if ok {
					d.openOfficeFile(path)
				}
			})
		}},
		{sep: true},
		{id: "prog", label: d.tr("程序(P)"), icon: ui.IcoFolderOpen, enabled: true, children: d.programEntries()},
		{id: "docs", label: d.tr("文档(D)"), icon: ui.IcoFolder, enabled: true, children: d.documentEntries()},
		{id: "settings", label: d.tr("设置(S)"), icon: ui.IcoSetup, enabled: true, children: d.settingsEntries()},
		{id: "find", label: d.tr("查找(F)"), icon: ui.IcoFolderOpen, enabled: true, action: func() { d.openFind() }},
		{id: "help", label: d.tr("帮助(H)"), icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
		{id: "run", label: d.tr("运行(R)…"), icon: ui.IcoSetup, enabled: true, action: func() { d.openRunDialog() }},
		{id: "nuke", label: "死手系统（核反击）…", icon: ui.IcoNuke, danger: true, enabled: true, action: func() { d.openDeadHand() }},
		{sep: true},
	}
	if d.Mode == ModeDesktop {
		entries = append(entries, startEntry{id: "sleep", label: d.tr("睡眠(S)…"), icon: ui.IcoComputer, enabled: true, action: func() { d.showSleep() }})
	}
	entries = append(entries,
		startEntry{id: "logoff", label: d.tr("注销(L)…"), icon: ui.IcoComputer, enabled: true, action: func() { d.showLogoff() }},
		startEntry{id: "restart", label: d.tr("重新启动(R)…"), icon: ui.IcoSetup, enabled: true, action: func() { d.showRestart() }},
		startEntry{id: "shutdown", label: d.tr("关闭计算机(U)…"), icon: ui.IcoComputer, enabled: true, action: func() { d.showShutdown() }},
	)
	return entries
}

func startEntriesHeight(entries []startEntry, itemH, sep int) int {
	h := 0
	for _, e := range entries {
		if e.sep {
			h += sep
		} else {
			h += itemH + 1
		}
	}
	return h
}

func (d *Desktop) startMenuHeight() int {
	h := startEntriesHeight(d.menuEntries(), 26, 9) + 8
	if maxH := d.taskbarTop() - 4; h > maxH {
		h = maxH
	}
	return h
}

func (d *Desktop) startMenuRect() ui.Rect {
	h := d.startMenuHeight()
	return ui.Rect{4, d.taskbarTop() - h - 2, startMenuW, h}
}

func (d *Desktop) startMenuViewport() ui.Rect {
	r := d.startMenuRect()
	v := ui.Rect{r.X + 30, r.Y + 4, r.W - 34, r.H - 8}
	if startEntriesHeight(d.menuEntries(), 26, 9) > v.H {
		v.W -= 18
	}
	return v
}

func (d *Desktop) startScrollMax() int {
	return maxInt(0, startEntriesHeight(d.menuEntries(), 26, 9)-d.startMenuViewport().H)
}

func (d *Desktop) scrollStart(delta int) {
	d.startScroll = maxInt(0, minInt(d.startScroll+delta, d.startScrollMax()))
	d.startSubDrag = false
	d.Invalidate()
}

func (d *Desktop) startItemRects() []ui.Rect {
	r := d.startMenuRect()
	v := d.startMenuViewport()
	var out []ui.Rect
	y := r.Y + 6 - d.startScroll
	for _, e := range d.menuEntries() {
		if e.sep {
			y += 9
			out = append(out, ui.Rect{})
			continue
		}
		out = append(out, ui.Rect{v.X, y, v.W, 26})
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
	d.startScroll = maxInt(0, minInt(d.startScroll, d.startScrollMax()))
	rects := d.startItemRects()
	viewport := d.startMenuViewport()
	y := r.Y + 6 - d.startScroll
	for i, e := range entries {
		if e.sep {
			if y+9 > viewport.Y && y < viewport.Bottom() {
				c.HLine(viewport.X+4, y+4, viewport.W-8, ui.ColShadow)
				c.HLine(viewport.X+4, y+5, viewport.W-8, ui.ColWhite)
			}
			y += 9
			continue
		}
		ir := rects[i]
		if ir.Bottom() > viewport.Y && ir.Y < viewport.Bottom() {
			hover := d.startSel == e.id
			if hover && e.enabled {
				if e.danger {
					c.FillRect(ir, 0x800000)
				} else {
					c.FillRect(ir, ui.ColNavy)
				}
			}
			ui.DrawIcon16(c, e.icon, ir.X+4, ir.Y+5)
			col := ui.ColText
			if e.danger {
				col = 0xA00000
			}
			if !e.enabled {
				col = ui.ColTextDis
			}
			if hover && e.enabled {
				col = ui.ColWhite
			}
			avail := ir.W - 26
			if len(e.children) > 0 {
				avail -= 16
			}
			f.TextClip(c, ir.X+26, ir.Y+5, avail, e.label, col)
			if len(e.children) > 0 {
				ac := ui.ColText
				if hover {
					ac = ui.ColWhite
				}
				ui.Arrow(c, ir.Right()-12, ir.Y+11, "right", ac)
			}
		}
		y += 27
	}
	if d.startScrollMax() > 0 {
		bar := ui.Rect{r.Right() - 18, r.Y + 3, 16, r.H - 6}
		ui.ScrollBar(c, bar, startEntriesHeight(entries, 26, 9), viewport.H, d.startScroll)
	}

	if d.startSub != "" {
		for i, e := range entries {
			if e.id != d.startSub || len(e.children) == 0 {
				continue
			}
			sr := d.startSubRect(rects[i], e)
			c.FillRect(sr, ui.ColFace)
			c.Raised(sr)
			view := d.startSubViewport(sr, e)
			d.startSubScroll = maxInt(0, minInt(d.startSubScroll, d.startSubScrollMax(sr, e)))
			yy := sr.Y + 4 - d.startSubScroll
			for _, sub := range e.children {
				if sub.sep {
					if yy+9 > view.Y && yy < view.Bottom() {
						c.HLine(view.X+4, yy+4, view.W-8, ui.ColShadow)
						c.HLine(view.X+4, yy+5, view.W-8, ui.ColWhite)
					}
					yy += 9
					continue
				}
				rr := ui.Rect{view.X, yy, view.W, 25}
				if rr.Bottom() > view.Y && rr.Y < view.Bottom() {
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
					f.TextClip(c, rr.X+28, rr.Y+5, rr.W-34, sub.label, col)
				}
				yy += 26
			}
			if d.startSubScrollMax(sr, e) > 0 {
				bar := ui.Rect{sr.Right() - 18, sr.Y + 3, 16, sr.H - 6}
				ui.ScrollBar(c, bar, startEntriesHeight(e.children, 25, 9), view.H, d.startSubScroll)
			}
		}
	}
}

func (d *Desktop) startSubRect(anchor ui.Rect, e startEntry) ui.Rect {
	h := startEntriesHeight(e.children, 25, 9) + 8
	if maxH := d.taskbarTop() - 4; h > maxH {
		h = maxH
	}
	x := d.startMenuRect().Right() - 4
	w := 226
	if x+w > d.W-4 {
		x = d.startMenuRect().X + 4 - w
	}
	y := anchor.Y - 2
	if y+h > d.taskbarTop()-2 {
		y = d.taskbarTop() - 2 - h
	}
	if y < 2 {
		y = 2
	}
	return ui.Rect{x, y, w, h}
}

func (d *Desktop) startSubViewport(sr ui.Rect, e startEntry) ui.Rect {
	v := ui.Rect{sr.X + 4, sr.Y + 4, sr.W - 8, sr.H - 8}
	if startEntriesHeight(e.children, 25, 9) > v.H {
		v.W -= 18
	}
	return v
}

func (d *Desktop) startSubScrollMax(sr ui.Rect, e startEntry) int {
	return maxInt(0, startEntriesHeight(e.children, 25, 9)-d.startSubViewport(sr, e).H)
}

func (d *Desktop) scrollStartSub(delta int) {
	for i, e := range d.menuEntries() {
		if e.id != d.startSub || len(e.children) == 0 {
			continue
		}
		sr := d.startSubRect(d.startItemRects()[i], e)
		d.startSubScroll = maxInt(0, minInt(d.startSubScroll+delta, d.startSubScrollMax(sr, e)))
		d.startSubDrag = false
		d.Invalidate()
		return
	}
}

func (d *Desktop) setStartSub(id string) {
	if d.startSub != id {
		d.startSubScroll = 0
	}
	d.startSub = id
}

func (d *Desktop) startSubScrollbar() (bar, thumb ui.Rect, entry startEntry, ok bool) {
	entries := d.menuEntries()
	rects := d.startItemRects()
	for i, e := range entries {
		if e.id != d.startSub || len(e.children) == 0 {
			continue
		}
		sr := d.startSubRect(rects[i], e)
		if d.startSubScrollMax(sr, e) == 0 {
			return ui.Rect{}, ui.Rect{}, startEntry{}, false
		}
		bar = ui.Rect{sr.Right() - 18, sr.Y + 3, 16, sr.H - 6}
		thumb = ui.ScrollbarThumbRect(bar, startEntriesHeight(e.children, 25, 9), d.startSubViewport(sr, e).H, d.startSubScroll)
		return bar, thumb, e, true
	}
	return ui.Rect{}, ui.Rect{}, startEntry{}, false
}

func (d *Desktop) dragStartSubTo(y int) {
	bar, thumb, entry, ok := d.startSubScrollbar()
	if !ok {
		return
	}
	rects := d.startItemRects()
	for i, e := range d.menuEntries() {
		if e.id != d.startSub {
			continue
		}
		sr := d.startSubRect(rects[i], e)
		maxPos := d.startSubScrollMax(sr, entry)
		trackH := bar.H - 36
		if trackH <= thumb.H {
			d.startSubScroll = 0
		} else {
			d.startSubScroll = (y - bar.Y - 18 - d.startSubDragOffset) * maxPos / (trackH - thumb.H)
			d.startSubScroll = maxInt(0, minInt(d.startSubScroll, maxPos))
		}
		d.Invalidate()
		return
	}
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
		view := d.startSubViewport(sr, e)
		yy := sr.Y + 4 - d.startSubScroll
		for _, sub := range e.children {
			if sub.sep {
				yy += 9
				continue
			}
			if (ui.Rect{view.X, yy, view.W, 25}).Contains(x, y) {
				return e.id, sub.id
			}
			yy += 26
		}
	}
	return "", ""
}

func (d *Desktop) startItemAt(x, y int) string {
	if !d.startMenuViewport().Contains(x, y) {
		return ""
	}
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
		"Windows 95 Linux 中文版\n版本 26.5 · ARM64 / UEFI\n\n"+
			"经典 Win95 外壳：桌面图标与开始菜单、收件箱、我的公文包、游戏、\n"+
			"在线服务、AOL/MSN 离线拨号演示、Netscape Navigator 4.8 安装向导。\n"+
			"极速全盘搜索与聚光灯（Ctrl+Space）、极客系统监视器、\n"+
			"连接监视器与防火墙、局域网快传、系统级 VNC、宿主机 Z: 映射、\n"+
			"按键精灵宏录制、DOOM 95 光线投射迷宫、截屏并打码。\n"+
			"AI 助手：自然语言转 Linux 命令（危险命令二次确认）与赛博义体对话；\n"+
			"Wine 兼容层可按需安装并运行 Windows 程序。\n"+
			"多语言与设置中心、纸牌、扫雷、三维弹球、3D 体素方块世界、画图、\n"+
			"PowerShell、JavaScript 浏览器、图片查看器、屏幕键盘、\n"+
			"系统更新（OTA）、Office 365 全件套（组件按需安装）、苏尔科视觉整活。\n"+
			"Arch Linux ARM glibc 子系统与真实 pacman（清华 TUNA 源），\n"+
			"可从开始菜单安装并启动官方微信 ARM64 版。\n"+
			"浏览器支持图片、彩色控件与表单；mcjs.link 的 Minecraft 通过\n"+
			"嵌套在 Win95 窗口里的 Firefox（Mesa WebGL2）运行。\n"+
			"X11 嵌套代理用 Xvfb :1 + XGetImage/XTest，在 Win95 窗口里\n"+
			"直接操作 X11 程序；需要完整桌面时，双击桌面的\n"+
			"“切换到 XFCE 桌面”交给 Xorg/XFCE，注销后返回 Win95。\n\n"+d.Host.Info,
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
		{id: "office", label: "Office 365 全件套", icon: ui.IcoOffice, enabled: true, action: func() { d.openOffice() }},
		{id: "office-setup", label: "Office 组件管理", icon: ui.IcoOffice, enabled: true, action: func() { d.openOfficeSetup("", "", "") }},
		{sep: true},
		{id: "inbox", label: "收件箱", icon: ui.IcoMail, enabled: true, action: func() { d.openInbox() }},
		{id: "briefcase", label: "我的公文包", icon: ui.IcoBriefcase, enabled: true, action: func() { d.openBriefcase() }},
		{id: "games", label: "游戏", icon: ui.IcoGame, enabled: true, action: func() { d.openGames() }},
		{id: "online", label: "在线服务", icon: ui.IcoOnline, enabled: true, action: func() { d.openOnlineServices() }},
		{id: "netscape", label: "Netscape Navigator 4.8", icon: ui.IcoNetscape, enabled: true, action: func() { d.openNetscape() }},
		{id: "ai", label: "AI 助手（命令 / 赛博义体）", icon: ui.IcoAI, enabled: true, action: func() { d.openAI(0) }},
		{id: "agent", label: "Agent Studio（智能体工作台）", icon: ui.IcoAI, enabled: true, action: func() { d.openAgentStudio() }},
		{id: "wine", label: "Wine 兼容层（Windows 程序）", icon: ui.IcoWine, enabled: true, action: func() { d.openWine() }},
		{sep: true},
		{id: "notepad", label: d.tr("记事本"), icon: ui.IcoNotepad, enabled: true, action: func() { d.openNotepad("") }},
		{id: "calc", label: d.tr("计算器"), icon: ui.IcoCalc, enabled: true, action: func() { d.openCalc() }},
		{id: "calendar", label: d.tr("日历"), icon: ui.IcoCalendar, enabled: true, action: func() { d.openCalendar() }},
		{id: "term", label: d.tr("终端"), icon: ui.IcoTerminal, enabled: true, action: func() { d.openTerminal() }},
		{id: "pshell", label: "Windows PowerShell", icon: ui.IcoShell, enabled: true, action: func() { d.openPowerShell() }},
		{id: "ise", label: "Windows PowerShell ISE", icon: ui.IcoISE, enabled: true, action: func() { d.openISE() }},
		{id: "files", label: d.tr("文件管理器"), icon: ui.IcoFolder, enabled: true, action: func() { d.openFiles("/root") }},
		{id: "archive", label: "万能压缩（ZIP / 7z / RAR / TAR）", icon: ui.IcoPackage, enabled: true, action: func() { d.openArchiveManager("") }},
		{id: "browser", label: d.tr("网络浏览器"), icon: ui.IcoBrowser, enabled: true, action: func() { d.openBrowser("") }},
		{id: "netcenter", label: "网络属性", icon: ui.IcoNetwork, enabled: true, action: func() { d.openNetworkCenter(0) }},
		{id: "netmon", label: "连接监视器与防火墙", icon: ui.IcoShield, enabled: true, action: func() { d.openNetworkCenter(1) }},
		{id: "lanshare", label: "局域网快传（手机互传）", icon: ui.IcoShare, enabled: true, action: func() { d.openLANShare() }},
		{id: "vnc", label: "系统级 VNC 服务器", icon: ui.IcoVNC, enabled: true, action: func() { d.openVNCServer() }},
		{id: "macro", label: "按键精灵（宏录制）", icon: ui.IcoMacro, enabled: true, action: func() { d.openMacro() }},
		{id: "screenrecorder", label: d.tr("屏幕录像机"), icon: ui.IcoMacro, enabled: true, action: func() { d.openScreenRecorder() }},
		{id: "mediaplayer", label: d.tr("媒体播放器"), icon: ui.IcoFile, enabled: true, action: func() { d.openMediaPlayer() }},
		{id: "taskmgr", label: d.tr("任务管理器"), icon: ui.IcoComputer, enabled: true, action: func() { d.openTaskManager() }},
		{id: "geekmonitor", label: "硬件资源与传感器", icon: ui.IcoMonitor, enabled: true, action: func() { d.openGeekMonitor() }},
		{id: "packetsniff", label: "局域网抓包与防火墙", icon: ui.IcoShield, enabled: true, action: func() { d.openPacketSniffer() }},
		{id: "diskmanager", label: "DiskGenius 风格磁盘管理", icon: ui.IcoComputer, enabled: true, action: func() { d.openDiskManager() }},
		{id: "snapshots", label: "系统快照与回滚", icon: ui.IcoPackage, enabled: true, action: func() { d.openSnapshotManager() }},
		{id: "regedit", label: d.tr("注册表编辑器"), icon: ui.IcoSetup, enabled: true, action: func() { d.openRegistryEditor() }},
		{id: "x11proxy", label: "X11 嵌套代理", icon: ui.IcoComputer, enabled: true, action: func() { d.openX11Proxy("") }},
		{id: "xfce-desktop", label: "切换到 XFCE 桌面", icon: ui.IcoSetup, enabled: true, action: func() { d.switchToXFCE() }},
		{id: "wechat", label: "微信 WeChat", icon: ui.IcoNetwork, enabled: true, action: func() { d.openWeChat() }},
		{sep: true},
		{id: "sol", label: d.tr("纸牌"), icon: ui.IcoCards, enabled: true, action: func() { d.openSolitaire() }},
		{id: "mines", label: d.tr("扫雷"), icon: ui.IcoMine, enabled: true, action: func() { d.openMines() }},
		{id: "blockworld", label: d.tr("方块世界 95"), icon: ui.IcoGame, enabled: true, action: func() { d.openBlockWorld() }},
		{id: "pinball", label: d.tr("三维弹球"), icon: ui.IcoGame, enabled: true, action: func() { d.openPinball() }},
		{id: "doom", label: "DOOM 95（光线投射）", icon: ui.IcoMaze, enabled: true, action: func() { d.openDoom() }},
		{id: "paint", label: d.tr("画图"), icon: ui.IcoPaint, enabled: true, action: func() { d.openPaint() }},
		{id: "imgview", label: d.tr("图片查看器"), icon: ui.IcoImage, enabled: true, action: func() { d.openImageViewer("") }},
		{sep: true},
		{id: "download", label: d.tr("图形化下载器"), icon: ui.IcoDownload, enabled: true, action: func() { d.openDownloader() }},
		{id: "pkgman", label: d.tr("软件包管理器"), icon: ui.IcoPackage, enabled: true, action: func() { d.openPkgMan() }},
		{id: "ota", label: "系统更新", icon: ui.IcoPackage, enabled: true, action: func() { d.openOTA() }},
		{id: "arch", label: "Arch Linux (glibc / pacman)", icon: ui.IcoPackage, enabled: true, action: func() { d.installArch() }},
		{id: "settings", label: d.tr("设置中心"), icon: ui.IcoSetup, enabled: true, action: func() { d.openSettings() }},
		{sep: true},
		{id: "osk", label: d.tr("屏幕键盘"), icon: ui.IcoKeyboard, enabled: true, action: func() { d.toggleSoftKeyboard() }},
		{sep: true},
		{id: "xfce-term", label: "XFCE 终端（X11 窗口）", icon: ui.IcoTerminal, enabled: true, action: func() { d.openX11Proxy("xfce4-terminal") }},
		{id: "firefox", label: "Firefox（X11 窗口）", icon: ui.IcoBrowser, enabled: true, action: func() { d.openX11Proxy("firefox") }},
		{id: "sysinfo", label: "系统信息", icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
		{sep: true},
		{id: "prank", label: "视觉整活：苏尔科…", icon: ui.IcoGlitch, danger: true, enabled: true, action: func() { d.openPrankDemo() }},
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
		{id: "settings", label: d.tr("设置中心"), icon: ui.IcoSetup, enabled: true, action: func() { d.openSettings() }},
		{id: "office-setup", label: "Office 组件管理", icon: ui.IcoOffice, enabled: true, action: func() { d.openOfficeSetup("", "", "") }},
		{id: "about", label: d.tr("关于本机"), icon: ui.IcoInfo, enabled: true, action: func() { d.showAbout() }},
		{id: "display", label: d.tr("显示属性(D)"), icon: ui.IcoComputer, enabled: true, action: func() { d.openDisplayProperties() }},
		{id: "shot", label: "截屏并打开画图(S)  Print Screen", icon: ui.IcoComputer, enabled: true, action: func() { d.printScreenToPaint() }},
		{id: "osk", label: d.tr("屏幕键盘") + "  Ctrl+Alt+K", icon: ui.IcoKeyboard, enabled: true, action: func() { d.toggleSoftKeyboard() }},
		{id: "pkgman", label: "添加/删除软件", icon: ui.IcoPackage, enabled: true, action: func() { d.openPkgMan() }},
		{id: "ota", label: "Windows 更新", icon: ui.IcoPackage, enabled: true, action: func() { d.openOTA() }},
		{id: "files", label: d.tr("文件管理器"), icon: ui.IcoFolder, enabled: true, action: func() { d.openFiles("/root") }},
		{sep: true},
		{id: "x11proxy", label: "X11 嵌套代理", icon: ui.IcoComputer, enabled: true, action: func() { d.openX11Proxy("") }},
		{id: "xfce-desktop", label: "切换到 XFCE 桌面", icon: ui.IcoSetup, enabled: true, action: func() { d.switchToXFCE() }},
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
	return fmt.Sprintf("%s %s\n%s", d.formatDate(time.Now()), d.clock, d.clockZone)
}
