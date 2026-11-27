package desktop

import (
	"fmt"
	"sort"
	"time"

	"win95linux/internal/ui"
)

var saverNames = []string{"无", "变幻线", "星空", "飞行窗口"}

var waitChoices = []int{1, 2, 3, 5, 10, 15, 20, 30, 45, 60}

type DisplayApp struct {
	Base
	d   *Desktop
	win *Window

	tab         int
	bg          int
	style       int
	wait        int
	origBg      int
	origStyle   int
	origWait    int
	origBgColor bool
	committed   bool
	comboOpen   int // 0 none, 1 saver, 2 wait, 3 resolution, 4 refresh, 5 rotation
	comboScroll VScroll
	bgScroll    VScroll
	press       string

	okR, cancelR, applyR, previewR ui.Rect
	bgListR                        ui.Rect
	styleBoxR, waitBoxR            ui.Rect
	resolutionR, refreshR, rotateR ui.Rect
	modes                          []DisplayMode
	mode                           DisplayMode
	modeIndex, refreshIndex        int
	rotationIndex                  int
	tabs                           [3]ui.Rect
}

func (d *Desktop) openDisplayProperties() {
	a := &DisplayApp{
		d:           d,
		bg:          d.background,
		style:       d.saverStyle,
		wait:        int(d.saverWait / time.Minute),
		origBg:      d.background,
		origStyle:   d.saverStyle,
		origWait:    int(d.saverWait / time.Minute),
		origBgColor: d.bgColorSet,
		mode:        d.displayMode,
	}
	a.modes = d.availableDisplayModes()
	a.syncDisplayMode()
	a.win = d.OpenModal(a)
}

func (a *DisplayApp) Title() string           { return a.d.tr("显示 属性") }
func (a *DisplayApp) Icon() ui.IconKind       { return ui.IcoComputer }
func (a *DisplayApp) DefaultSize() (int, int) { return 600, 478 }
func (a *DisplayApp) MinSize() (int, int)     { return 480, 440 }
func (a *DisplayApp) Resizable() bool         { return false }

func (a *DisplayApp) waitList() []int {
	opts := append([]int{}, waitChoices...)
	found := false
	for _, v := range opts {
		if v == a.wait {
			found = true
			break
		}
	}
	if !found {
		opts = append(opts, a.wait)
		sort.Ints(opts)
	}
	return opts
}

func (a *DisplayApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	tabY := r.Y + 8
	tabW := 118
	names := []string{d.tr("背景(B)"), d.tr("屏幕保护程序(S)"), d.tr("监视器(M)")}
	for i, label := range names {
		tr := ui.Rect{r.X + 10 + i*(tabW+2), tabY, tabW, 24}
		a.tabs[i] = tr
		c.FillRect(tr, ui.ColFace)
		c.HLine(tr.X, tr.Y, tr.W, ui.ColWhite)
		c.VLine(tr.X, tr.Y, tr.H+1, ui.ColWhite)
		c.HLine(tr.X+1, tr.Y+1, tr.W-2, ui.ColLight)
		if i == a.tab {
			c.HLine(tr.X+tr.W-1, tr.Y+1, 1, ui.ColWhite)
			c.FillRect(ui.Rect{tr.X + 2, tr.Bottom() - 1, tr.W - 2, 2}, ui.ColFace)
		} else {
			c.VLine(tr.Right()-1, tr.Y, tr.H, ui.ColShadow)
			c.HLine(tr.X+1, tr.Bottom()-1, tr.W-1, ui.ColShadow)
		}
		f.Text(c, tr.X+12, tr.Y+4, label, ui.ColText)
	}

	body := ui.Rect{r.X + 10, tabY + 24, r.W - 20, r.Bottom() - (tabY + 24) - 42}
	c.FillRect(body, ui.ColFace)
	c.HLine(body.X, body.Y, body.W, ui.ColWhite)
	c.VLine(body.X, body.Y, body.H, ui.ColWhite)
	c.HLine(body.X, body.Bottom()-1, body.W, ui.ColShadow)
	c.VLine(body.Right()-1, body.Y, body.H, ui.ColShadow)
	inner := body.Inset(10)

	if a.tab == 0 {
		a.paintBackgroundTab(c, f, inner)
	} else if a.tab == 1 {
		a.paintSaverTab(c, f, inner)
	} else {
		a.paintMonitorTab(c, f, inner)
	}

	btnW, gap := 84, 8
	by := r.Bottom() - 36
	a.applyR = ui.Rect{r.Right() - 10 - btnW, by, btnW, 26}
	a.cancelR = ui.Rect{a.applyR.X - btnW - gap, by, btnW, 26}
	a.okR = ui.Rect{a.cancelR.X - btnW - gap, by, btnW, 26}
	ui.Button(c, a.okR, d.tr("确定"), a.press == "ok", true)
	ui.Button(c, a.cancelR, d.tr("取消"), a.press == "cancel", true)
	ui.Button(c, a.applyR, d.tr("应用"), a.press == "apply", true)

	if a.comboOpen != 0 {
		a.drawComboList(c, r)
	}
}

func (a *DisplayApp) paintBackgroundTab(c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.Text(c, inner.X, inner.Y, a.d.tr("背景(B):"), ui.ColText)
	a.bgListR = ui.Rect{inner.X, inner.Y + 22, 190, inner.H - 30}
	c.FillRect(a.bgListR, ui.ColField)
	c.Sunken(a.bgListR)
	box := a.bgListR.Inset(2)
	scrollW := 16
	rows := (box.H - 2) / 22
	if rows < 1 {
		rows = 1
	}
	a.bgScroll.Total = len(backgroundStyles)
	a.bgScroll.Visible = rows
	a.bgScroll.Clamp()
	y := box.Y + 1
	for i := a.bgScroll.Pos; i < len(backgroundStyles) && y+21 <= box.Bottom(); i++ {
		rr := ui.Rect{box.X + 1, y, box.W - scrollW - 2, 21}
		if i == a.bg {
			c.FillRect(rr, ui.ColNavy)
		}
		col := ui.ColText
		if i == a.bg {
			col = ui.ColWhite
		}
		f.TextClip(c, rr.X+6, rr.Y+3, rr.W-10, a.d.tr(backgroundStyles[i]), col)
		y += 22
	}
	a.bgScroll.Draw(c, ui.Rect{box.Right() - scrollW - 1, box.Y + 1, scrollW, box.H - 2})

	mx := a.bgListR.Right() + 28
	mw := inner.Right() - mx
	if mw > 300 {
		mw = 300
	}
	mx = inner.Right() - mw
	bezel := ui.Rect{mx, inner.Y + 8, mw, mw * 3 / 4}
	c.FillRect(bezel, 0x808080)
	c.Raised(bezel)
	screen := ui.Rect{bezel.X + 10, bezel.Y + 10, bezel.W - 20, bezel.H - 32}
	drawBackgroundRect(c, screen, a.bg)
	c.Sunken(screen)
	standW := mw / 4
	c.FillRect(ui.Rect{bezel.X + (mw-standW)/2, bezel.Bottom(), standW, 12}, ui.ColFace)
	c.Raised(ui.Rect{bezel.X + (mw-standW)/2, bezel.Bottom(), standW, 12})
	baseW := mw / 3
	c.FillRect(ui.Rect{bezel.X + (mw-baseW)/2, bezel.Bottom() + 10, baseW, 6}, ui.ColFace)
	c.Raised(ui.Rect{bezel.X + (mw-baseW)/2, bezel.Bottom() + 10, baseW, 6})
	f.Text(c, bezel.X+4, bezel.Bottom()+24, a.d.trf("预览 · %s", a.d.tr(backgroundStyles[a.bg])), ui.ColText)
	f.TextClip(c, inner.X, inner.Bottom()-18, inner.W, a.d.tr("背景设置会在确认后立即应用到桌面。"), ui.ColShadow)
}

func (a *DisplayApp) paintSaverTab(c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	group := ui.Rect{inner.X, inner.Y + 10, inner.W, 176}
	ui.GroupBox(c, group, a.d.tr("屏幕保护程序(S)"))
	f.Text(c, group.X+18, group.Y+22, a.d.tr("屏幕保护程序(S):"), ui.ColText)
	a.styleBoxR = ui.Rect{group.X + 146, group.Y + 18, 210, 24}
	styleName := a.d.tr("无")
	if a.style >= 0 && a.style < len(saverNames) {
		styleName = a.d.tr(saverNames[a.style])
	}
	a.drawCombo(c, a.styleBoxR, styleName, a.comboOpen == 1)

	f.Text(c, group.X+18, group.Y+60, a.d.tr("等待(W):"), ui.ColText)
	a.waitBoxR = ui.Rect{group.X + 146, group.Y + 56, 70, 24}
	a.drawCombo(c, a.waitBoxR, fmt.Sprintf("%d", a.wait), a.comboOpen == 2)
	f.Text(c, a.waitBoxR.Right()+8, a.waitBoxR.Y+4, a.d.tr("分钟"), ui.ColText)
	a.previewR = ui.Rect{group.X + 306, group.Y + 55, 84, 26}
	ui.Button(c, a.previewR, a.d.tr("预览(V)"), a.press == "preview", a.style != 0)

	desc := a.d.tr("屏幕保护程序已关闭。")
	if a.style > 0 {
		name := a.d.tr(saverNames[a.style])
		desc = a.d.trf("在计算机闲置 %d 分钟后启动“%s”。", a.wait, name)
	}
	f.TextClip(c, group.X+18, group.Y+104, group.W-36, desc, ui.ColText)
	f.Text(c, group.X+18, group.Y+130, a.d.tr("移动鼠标或按任意键即可返回桌面。"), ui.ColShadow)

	note := a.d.tr("提示：预览会全屏显示屏保，任意输入即可停止。")
	f.TextClip(c, inner.X, inner.Bottom()-18, inner.W, note, ui.ColShadow)
}

func (a *DisplayApp) refreshChoices() []int {
	opts := []int{50, 60, 75}
	for _, v := range opts {
		if v == a.mode.Refresh {
			return opts
		}
	}
	opts = append(opts, a.mode.Refresh)
	sort.Ints(opts)
	return opts
}

func (a *DisplayApp) syncDisplayMode() {
	if len(a.modes) == 0 {
		a.modes = []DisplayMode{a.d.displayMode}
	}
	found := false
	for i, m := range a.modes {
		if m.Width == a.d.displayMode.Width && m.Height == a.d.displayMode.Height {
			a.mode = m
			a.mode.Refresh = a.d.displayMode.Refresh
			a.mode.Rotation = a.d.displayMode.Rotation
			a.modeIndex = i
			found = true
			break
		}
	}
	if !found {
		a.modes = append([]DisplayMode{a.d.displayMode}, a.modes...)
		a.mode = a.d.displayMode
		a.modeIndex = 0
	}
	opts := a.refreshChoices()
	a.refreshIndex = 0
	for i, v := range opts {
		if v == a.mode.Refresh {
			a.refreshIndex = i
			break
		}
	}
	a.rotationIndex = a.mode.Rotation / 90
}

func (a *DisplayApp) selectedDisplayMode() DisplayMode {
	mode := a.mode
	if a.modeIndex >= 0 && a.modeIndex < len(a.modes) {
		mode.Width = a.modes[a.modeIndex].Width
		mode.Height = a.modes[a.modeIndex].Height
	}
	opts := a.refreshChoices()
	if a.refreshIndex >= 0 && a.refreshIndex < len(opts) {
		mode.Refresh = opts[a.refreshIndex]
	}
	mode.Rotation = a.rotationIndex * 90
	return mode.Normalized()
}

func (a *DisplayApp) paintMonitorTab(c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	wide := inner.W >= 560
	groupH := 252
	if !wide {
		groupH = 292
	}
	group := ui.Rect{inner.X, inner.Y + 10, inner.W, groupH}
	ui.GroupBox(c, group, a.d.tr("监视器设置"))
	controlW := minInt(190, group.W-164)
	f.Text(c, group.X+18, group.Y+22, a.d.tr("屏幕区域(A):"), ui.ColText)
	a.resolutionR = ui.Rect{group.X + 146, group.Y + 18, controlW, 24}
	resolution := a.d.tr("未知")
	if a.modeIndex >= 0 && a.modeIndex < len(a.modes) {
		m := a.modes[a.modeIndex]
		resolution = a.d.trf("%d × %d 像素", m.Width, m.Height)
	}
	a.drawCombo(c, a.resolutionR, resolution, a.comboOpen == 3)

	f.Text(c, group.X+18, group.Y+60, a.d.tr("刷新频率(R):"), ui.ColText)
	a.refreshR = ui.Rect{group.X + 146, group.Y + 56, 100, 24}
	opts := a.refreshChoices()
	refresh := 60
	if a.refreshIndex >= 0 && a.refreshIndex < len(opts) {
		refresh = opts[a.refreshIndex]
	}
	a.drawCombo(c, a.refreshR, fmt.Sprintf("%d Hz", refresh), a.comboOpen == 4)

	f.Text(c, group.X+18, group.Y+98, a.d.tr("转向(O):"), ui.ColText)
	a.rotateR = ui.Rect{group.X + 146, group.Y + 94, controlW, 24}
	a.drawCombo(c, a.rotateR, a.d.tr(displayRotationLabel(a.rotationIndex*90)), a.comboOpen == 5)

	infoX, infoY := group.X+18, group.Y+144
	note1Y, note2Y := group.Y+172, group.Y+198
	if wide {
		preview := ui.Rect{group.X + 366, group.Y + 20, 156, 126}
		drawMonitorPreview(c, preview, a.selectedDisplayMode())
	} else {
		preview := ui.Rect{group.X + 18, group.Y + 128, 112, 92}
		drawMonitorPreview(c, preview, a.selectedDisplayMode())
		infoX = group.X + 146
		note1Y, note2Y = group.Y+232, group.Y+258
	}
	current := a.d.trf("当前模式: %d×%d · %d Hz · %d°", a.d.displayMode.Width, a.d.displayMode.Height, a.d.displayMode.Refresh, a.d.displayMode.Rotation)
	f.TextClip(c, infoX, infoY, group.Right()-18-infoX, current, ui.ColText)
	f.TextClip(c, group.X+18, note1Y, group.W-36, a.d.tr("分辨率和刷新率由显卡/虚拟机驱动决定；不支持时会自动恢复。"), ui.ColShadow)
	f.TextClip(c, group.X+18, note2Y, group.W-36, a.d.tr("转向由 Win95 桌面渲染并同步鼠标坐标，不改变 XFCE 桌面方向。"), ui.ColShadow)

	supported := a.d.trf("驱动返回 %d 个分辨率", len(a.modes))
	if a.d.Host.SetDisplayMode == nil {
		supported = a.d.tr("当前主机没有动态显示接口")
	}
	f.TextClip(c, inner.X, inner.Bottom()-18, inner.W, a.d.trf("%s。设置成功后自动保存。", supported), ui.ColShadow)
}

func (a *DisplayApp) drawCombo(c *ui.Canvas, r ui.Rect, text string, open bool) {
	f := ui.DefaultFont()
	c.FillRect(r, ui.ColField)
	c.Sunken(r)
	f.TextClip(c, r.X+5, r.Y+4, r.W-28, text, ui.ColText)
	arrow := ui.Rect{r.Right() - 19, r.Y + 1, 18, r.H - 2}
	ui.Button(c, arrow, "", open, true)
	ui.Arrow(c, arrow.X+7, arrow.Y+7, "down", ui.ColText)
}

func (a *DisplayApp) comboListRect(r ui.Rect, n int) ui.Rect {
	anchor := a.styleBoxR
	switch a.comboOpen {
	case 2:
		anchor = a.waitBoxR
	case 3:
		anchor = a.resolutionR
	case 4:
		anchor = a.refreshR
	case 5:
		anchor = a.rotateR
	}
	maxH := maxInt(64, r.H-80)
	h := minInt(n*20+4, maxH)
	y := anchor.Bottom()
	if y+h > r.Bottom()-40 {
		y = anchor.Y - h
	}
	if y < r.Y+32 {
		y = r.Y + 32
	}
	return ui.Rect{anchor.X, y, anchor.W, h}
}

func (a *DisplayApp) comboScrollRect() ui.Rect {
	box := a.comboListRect(a.win.ClientRect(), len(a.comboItems())).Inset(2)
	return ui.Rect{box.Right() - 17, box.Y, 16, box.H}
}

func (a *DisplayApp) selectedComboIndex() int {
	switch a.comboOpen {
	case 1:
		return a.style
	case 2:
		for i, v := range a.waitList() {
			if v == a.wait {
				return i
			}
		}
	case 3:
		return a.modeIndex
	case 4:
		return a.refreshIndex
	case 5:
		return a.rotationIndex
	}
	return 0
}

func (a *DisplayApp) openCombo(id int) {
	a.comboOpen = id
	a.comboScroll.Total = len(a.comboItems())
	a.comboScroll.Visible = 8
	a.comboScroll.Clamp()
	a.ensureComboVisible()
}

func (a *DisplayApp) ensureComboVisible() {
	if a.comboScroll.Visible < 1 {
		return
	}
	selected := a.selectedComboIndex()
	if selected < a.comboScroll.Pos {
		a.comboScroll.Pos = selected
	}
	if selected >= a.comboScroll.Pos+a.comboScroll.Visible {
		a.comboScroll.Pos = selected - a.comboScroll.Visible + 1
	}
	a.comboScroll.Clamp()
}

func (a *DisplayApp) comboItems() []string {
	switch a.comboOpen {
	case 1:
		items := make([]string, len(saverNames))
		for i, name := range saverNames {
			items[i] = a.d.tr(name)
		}
		return items
	case 2:
		items := make([]string, 0, len(a.waitList()))
		for _, v := range a.waitList() {
			items = append(items, a.d.trf("%d 分钟", v))
		}
		return items
	case 3:
		items := make([]string, 0, len(a.modes))
		for _, m := range a.modes {
			items = append(items, a.d.trf("%d × %d 像素", m.Width, m.Height))
		}
		return items
	case 4:
		items := make([]string, 0, len(a.refreshChoices()))
		for _, v := range a.refreshChoices() {
			items = append(items, fmt.Sprintf("%d Hz", v))
		}
		return items
	case 5:
		return []string{
			a.d.tr(displayRotationLabel(0)),
			a.d.tr(displayRotationLabel(90)),
			a.d.tr(displayRotationLabel(180)),
			a.d.tr(displayRotationLabel(270)),
		}
	default:
		return nil
	}
}

func (a *DisplayApp) drawComboList(c *ui.Canvas, r ui.Rect) {
	f := ui.DefaultFont()
	items := a.comboItems()
	lr := a.comboListRect(r, len(items))
	c.FillRect(lr, ui.ColField)
	c.Raised(lr)
	box := lr.Inset(2)
	a.comboScroll.Total = len(items)
	a.comboScroll.Visible = maxInt(1, (box.H-1)/20)
	a.comboScroll.Clamp()
	a.ensureComboVisible()
	scroll := a.comboScrollRect()
	a.comboScroll.Draw(c, scroll)
	y := box.Y + 1
	for i := a.comboScroll.Pos; i < len(items) && y+20 <= box.Bottom(); i++ {
		selected := i == a.selectedComboIndex()
		row := ui.Rect{box.X + 1, y, box.W - 19, 20}
		if selected {
			c.FillRect(row, ui.ColNavy)
		}
		col := ui.ColText
		if selected {
			col = ui.ColWhite
		}
		f.TextClip(c, row.X+5, row.Y+2, row.W-8, items[i], col)
		y += 20
	}
}

func (a *DisplayApp) comboIndexAt(x, y int) int {
	items := a.comboItems()
	lr := a.comboListRect(a.win.ClientRect(), len(items))
	if !lr.Contains(x, y) {
		return -1
	}
	idx := a.comboScroll.Pos + (y-lr.Y-3)/20
	if idx < a.comboScroll.Pos || idx >= len(items) || idx >= a.comboScroll.Pos+a.comboScroll.Visible {
		return -1
	}
	return idx
}

func (a *DisplayApp) bgScrollRect() ui.Rect {
	box := a.bgListR.Inset(2)
	return ui.Rect{box.Right() - 17, box.Y + 1, 16, box.H - 2}
}

func (a *DisplayApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	scroll := a.bgScrollRect()
	if ev.Kind == MouseWheel {
		if a.comboOpen != 0 {
			lr := a.comboListRect(client, len(a.comboItems()))
			if lr.Contains(ev.X, ev.Y) {
				if changed, _ := a.comboScroll.Mouse(ev, a.comboScrollRect()); changed {
					d.Invalidate()
				}
			}
		} else if a.bgListR.Contains(ev.X, ev.Y) {
			if changed, _ := a.bgScroll.Mouse(ev, scroll); changed {
				d.Invalidate()
			}
		}
		return
	}
	if a.comboOpen != 0 {
		if ev.Kind == MouseMove || ev.Kind == MouseUp {
			changed, consumed := a.comboScroll.Mouse(ev, a.comboScrollRect())
			if changed {
				d.Invalidate()
			}
			if consumed {
				return
			}
		}
		if ev.Kind == MouseDown && ev.Button == 1 {
			if a.comboScrollRect().Contains(ev.X, ev.Y) {
				a.comboScroll.Mouse(ev, a.comboScrollRect())
				d.Invalidate()
				return
			}
			if idx := a.comboIndexAt(ev.X, ev.Y); idx >= 0 {
				switch a.comboOpen {
				case 1:
					a.style = idx
				case 2:
					a.wait = a.waitList()[idx]
				case 3:
					a.modeIndex = idx
				case 4:
					a.refreshIndex = idx
				case 5:
					a.rotationIndex = idx
				}
			}
			a.comboOpen = 0
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseMove || ev.Kind == MouseUp {
		if changed, _ := a.bgScroll.Mouse(ev, scroll); changed {
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
		if a.bgListR.Contains(ev.X, ev.Y) && scroll.Contains(ev.X, ev.Y) {
			if changed, _ := a.bgScroll.Mouse(ev, scroll); changed {
				d.Invalidate()
			}
			return
		}
		switch {
		case a.tabs[0].Contains(ev.X, ev.Y):
			a.tab = 0
			d.Invalidate()
		case a.tabs[1].Contains(ev.X, ev.Y):
			a.tab = 1
			a.comboOpen = 0
			d.Invalidate()
		case a.tabs[2].Contains(ev.X, ev.Y):
			a.tab = 2
			a.comboOpen = 0
			d.Invalidate()
		case a.bgListR.Contains(ev.X, ev.Y):
			box := a.bgListR.Inset(2)
			row := (ev.Y - box.Y - 1) / 22
			idx := a.bgScroll.Pos + row
			if idx >= 0 && idx < len(backgroundStyles) && ev.Y < box.Bottom() {
				a.bg = idx
				d.Invalidate()
			}
		case a.styleBoxR.Contains(ev.X, ev.Y) && a.tab == 1:
			a.openCombo(1)
			d.Invalidate()
		case a.waitBoxR.Contains(ev.X, ev.Y) && a.tab == 1:
			a.openCombo(2)
			d.Invalidate()
		case a.resolutionR.Contains(ev.X, ev.Y) && a.tab == 2:
			a.openCombo(3)
			d.Invalidate()
		case a.refreshR.Contains(ev.X, ev.Y) && a.tab == 2:
			a.openCombo(4)
			d.Invalidate()
		case a.rotateR.Contains(ev.X, ev.Y) && a.tab == 2:
			a.openCombo(5)
			d.Invalidate()
		case a.previewR.Contains(ev.X, ev.Y):
			a.press = "preview"
		case a.okR.Contains(ev.X, ev.Y):
			a.press = "ok"
		case a.cancelR.Contains(ev.X, ev.Y):
			a.press = "cancel"
		case a.applyR.Contains(ev.X, ev.Y):
			a.press = "apply"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "preview":
			if a.previewR.Contains(ev.X, ev.Y) {
				a.preview()
			}
		case "ok":
			if a.okR.Contains(ev.X, ev.Y) {
				a.apply()
				a.committed = true
				a.d.CloseWindowForced(w)
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) {
				a.restore()
				a.committed = true
				a.d.CloseWindowForced(w)
			}
		case "apply":
			if a.applyR.Contains(ev.X, ev.Y) {
				a.apply()
				a.origBg, a.origStyle, a.origWait = a.bg, a.style, a.wait
				d.Invalidate()
			}
		}
	}
}

func (a *DisplayApp) apply() {
	if a.wait < 1 {
		a.wait = 1
	}
	a.d.background = a.bg
	a.d.bgColorSet = false
	a.d.saverStyle = a.style
	a.d.saverWait = time.Duration(a.wait) * time.Minute
	a.d.savePrefs()
	a.d.Invalidate()
	if a.d.Host.SetDisplayMode != nil {
		mode := a.selectedDisplayMode()
		if mode != a.d.displayMode {
			_ = a.d.applyDisplayMode(mode, true)
		}
	}
}

func (a *DisplayApp) restore() {
	a.d.background = a.origBg
	a.d.bgColorSet = a.origBgColor
	a.d.saverStyle = a.origStyle
	a.d.saverWait = time.Duration(a.origWait) * time.Minute
	a.d.Invalidate()
}

func (a *DisplayApp) preview() {
	if a.style == 0 {
		a.d.MessageBox(a.d.tr("预览"), a.d.tr("请先选择一种屏幕保护程序。"), IconInfo, []string{a.d.tr("确定")}, nil)
		return
	}
	a.d.background = a.bg
	a.d.saverStyle = a.style
	a.d.saverWait = time.Duration(a.wait) * time.Minute
	a.d.startSaver(time.Now())
	a.d.Invalidate()
}

func (a *DisplayApp) Close() {
	if !a.committed {
		a.restore()
	}
}

func (a *DisplayApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.comboOpen != 0 {
		switch ev.Name {
		case "Esc", "Enter":
			a.comboOpen = 0
			d.Invalidate()
			return true
		case "Up", "Down":
			delta := 1
			if ev.Name == "Up" {
				delta = -1
			}
			switch a.comboOpen {
			case 1:
				a.style = (a.style + len(saverNames) + delta) % len(saverNames)
			case 2:
				opts := a.waitList()
				idx := 0
				for i, v := range opts {
					if v == a.wait {
						idx = i
						break
					}
				}
				idx += delta
				if idx < 0 {
					idx = 0
				}
				if idx >= len(opts) {
					idx = len(opts) - 1
				}
				a.wait = opts[idx]
			case 3:
				a.modeIndex = maxInt(0, minInt(a.modeIndex+delta, len(a.modes)-1))
			case 4:
				opts := a.refreshChoices()
				a.refreshIndex = maxInt(0, minInt(a.refreshIndex+delta, len(opts)-1))
			case 5:
				a.rotationIndex = (a.rotationIndex + 4 + delta) % 4
			}
			a.ensureComboVisible()
			d.Invalidate()
			return true
		}
		return true
	}
	switch ev.Name {
	case "Esc":
		a.restore()
		a.committed = true
		a.d.CloseWindowForced(w)
		return true
	case "Enter":
		a.apply()
		a.committed = true
		a.d.CloseWindowForced(w)
		return true
	case "Tab", "Left", "Right":
		a.tab = (a.tab + 1) % len(a.tabs)
		a.comboOpen = 0
		d.Invalidate()
		return true
	case "Up", "Down":
		if a.tab == 0 {
			if ev.Name == "Up" && a.bg > 0 {
				a.bg--
			}
			if ev.Name == "Down" && a.bg < len(backgroundStyles)-1 {
				a.bg++
			}
			a.ensureBgVisible()
			d.Invalidate()
			return true
		}
		if a.tab == 1 {
			if ev.Name == "Up" && a.style > 0 {
				a.style--
			}
			if ev.Name == "Down" && a.style < len(saverNames)-1 {
				a.style++
			}
			d.Invalidate()
			return true
		}
		delta := 1
		if ev.Name == "Up" {
			delta = -1
		}
		a.modeIndex = maxInt(0, minInt(a.modeIndex+delta, len(a.modes)-1))
		d.Invalidate()
		return true
	}
	return false
}

func (a *DisplayApp) ensureBgVisible() {
	rows := a.bgScroll.Visible
	if rows < 1 {
		rows = 1
	}
	if a.bg < a.bgScroll.Pos {
		a.bgScroll.Pos = a.bg
	}
	if a.bg >= a.bgScroll.Pos+rows {
		a.bgScroll.Pos = a.bg - rows + 1
	}
}
