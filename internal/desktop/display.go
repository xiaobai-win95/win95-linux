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

	tab       int
	bg        int
	style     int
	wait      int
	origBg    int
	origStyle int
	origWait  int
	committed bool
	comboOpen int // 0 none, 1 saver, 2 wait
	bgScroll  VScroll
	press     string

	okR, cancelR, applyR, previewR ui.Rect
	bgListR                        ui.Rect
	styleBoxR, waitBoxR            ui.Rect
	tabs                           [2]ui.Rect
}

func (d *Desktop) openDisplayProperties() {
	a := &DisplayApp{
		d:         d,
		bg:        d.background,
		style:     d.saverStyle,
		wait:      int(d.saverWait / time.Minute),
		origBg:    d.background,
		origStyle: d.saverStyle,
		origWait:  int(d.saverWait / time.Minute),
	}
	a.win = d.OpenModal(a)
}

func (a *DisplayApp) Title() string           { return "显示 属性" }
func (a *DisplayApp) Icon() ui.IconKind       { return ui.IcoComputer }
func (a *DisplayApp) DefaultSize() (int, int) { return 560, 438 }
func (a *DisplayApp) MinSize() (int, int)     { return 560, 438 }
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
	names := []string{"背景(B)", "屏幕保护程序(S)"}
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
	} else {
		a.paintSaverTab(c, f, inner)
	}

	btnW, gap := 84, 8
	by := r.Bottom() - 36
	a.applyR = ui.Rect{r.Right() - 10 - btnW, by, btnW, 26}
	a.cancelR = ui.Rect{a.applyR.X - btnW - gap, by, btnW, 26}
	a.okR = ui.Rect{a.cancelR.X - btnW - gap, by, btnW, 26}
	ui.Button(c, a.okR, "确定", a.press == "ok", true)
	ui.Button(c, a.cancelR, "取消", a.press == "cancel", true)
	ui.Button(c, a.applyR, "应用", a.press == "apply", true)

	if a.comboOpen != 0 {
		a.drawComboList(c, r)
	}
}

func (a *DisplayApp) paintBackgroundTab(c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.Text(c, inner.X, inner.Y, "背景(B):", ui.ColText)
	a.bgListR = ui.Rect{inner.X, inner.Y + 22, 190, inner.H - 30}
	c.FillRect(a.bgListR, ui.ColWhite)
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
		f.TextClip(c, rr.X+6, rr.Y+3, rr.W-10, backgroundStyles[i], col)
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
	f.Text(c, bezel.X+4, bezel.Bottom()+24, "预览 · "+backgroundStyles[a.bg], ui.ColText)
	f.TextClip(c, inner.X, inner.Bottom()-18, inner.W, "背景设置会在确认后立即应用到桌面。", ui.ColShadow)
}

func (a *DisplayApp) paintSaverTab(c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	group := ui.Rect{inner.X, inner.Y + 10, inner.W, 176}
	ui.GroupBox(c, group, "屏幕保护程序(S)")
	f.Text(c, group.X+18, group.Y+22, "屏幕保护程序(S):", ui.ColText)
	a.styleBoxR = ui.Rect{group.X + 146, group.Y + 18, 210, 24}
	styleName := "无"
	if a.style >= 0 && a.style < len(saverNames) {
		styleName = saverNames[a.style]
	}
	a.drawCombo(c, a.styleBoxR, styleName, a.comboOpen == 1)

	f.Text(c, group.X+18, group.Y+60, "等待(W):", ui.ColText)
	a.waitBoxR = ui.Rect{group.X + 146, group.Y + 56, 70, 24}
	a.drawCombo(c, a.waitBoxR, fmt.Sprintf("%d", a.wait), a.comboOpen == 2)
	f.Text(c, a.waitBoxR.Right()+8, a.waitBoxR.Y+4, "分钟", ui.ColText)
	a.previewR = ui.Rect{group.X + 306, group.Y + 55, 84, 26}
	ui.Button(c, a.previewR, "预览(V)", a.press == "preview", a.style != 0)

	desc := "屏幕保护程序已关闭。"
	if a.style > 0 {
		name := saverNames[a.style]
		desc = fmt.Sprintf("在计算机闲置 %d 分钟后启动“%s”。", a.wait, name)
	}
	f.TextClip(c, group.X+18, group.Y+104, group.W-36, desc, ui.ColText)
	f.Text(c, group.X+18, group.Y+130, "移动鼠标或按任意键即可返回桌面。", ui.ColShadow)

	note := "提示：预览会全屏显示屏保，任意输入即可停止。"
	f.TextClip(c, inner.X, inner.Bottom()-18, inner.W, note, ui.ColShadow)
}

func (a *DisplayApp) drawCombo(c *ui.Canvas, r ui.Rect, text string, open bool) {
	f := ui.DefaultFont()
	c.FillRect(r, ui.ColWhite)
	c.Sunken(r)
	f.TextClip(c, r.X+5, r.Y+4, r.W-28, text, ui.ColText)
	arrow := ui.Rect{r.Right() - 19, r.Y + 1, 18, r.H - 2}
	ui.Button(c, arrow, "", open, true)
	ui.Arrow(c, arrow.X+7, arrow.Y+7, "down", ui.ColText)
}

func (a *DisplayApp) comboListRect(r ui.Rect, n int) ui.Rect {
	anchor := a.styleBoxR
	if a.comboOpen == 2 {
		anchor = a.waitBoxR
	}
	h := n*20 + 4
	y := anchor.Bottom()
	if y+h > r.Bottom()-40 {
		y = anchor.Y - h
	}
	if y < r.Y+2 {
		y = r.Y + 2
	}
	return ui.Rect{anchor.X, y, anchor.W, h}
}

func (a *DisplayApp) drawComboList(c *ui.Canvas, r ui.Rect) {
	f := ui.DefaultFont()
	var items []string
	if a.comboOpen == 1 {
		items = saverNames
	} else {
		for _, v := range a.waitList() {
			items = append(items, fmt.Sprintf("%d 分钟", v))
		}
	}
	lr := a.comboListRect(r, len(items))
	c.FillRect(lr, ui.ColWhite)
	c.Raised(lr)
	y := lr.Y + 2
	for i, label := range items {
		selected := false
		if a.comboOpen == 1 {
			selected = i == a.style
		} else {
			opts := a.waitList()
			selected = i < len(opts) && opts[i] == a.wait
		}
		row := ui.Rect{lr.X + 2, y, lr.W - 4, 20}
		if selected {
			c.FillRect(row, ui.ColNavy)
		}
		col := ui.ColText
		if selected {
			col = ui.ColWhite
		}
		f.TextClip(c, row.X+5, row.Y+2, row.W-8, label, col)
		y += 20
	}
}

func (a *DisplayApp) comboIndexAt(x, y int) int {
	var n int
	if a.comboOpen == 1 {
		n = len(saverNames)
	} else {
		n = len(a.waitList())
	}
	lr := a.comboListRect(a.win.ClientRect(), n)
	if !lr.Contains(x, y) {
		return -1
	}
	idx := (y - lr.Y - 2) / 20
	if idx < 0 || idx >= n {
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
		if a.comboOpen == 0 && a.bgListR.Contains(ev.X, ev.Y) {
			if changed, _ := a.bgScroll.Mouse(ev, scroll); changed {
				d.Invalidate()
			}
		}
		return
	}
	if a.comboOpen != 0 {
		if ev.Kind == MouseDown && ev.Button == 1 {
			if idx := a.comboIndexAt(ev.X, ev.Y); idx >= 0 {
				if a.comboOpen == 1 {
					a.style = idx
				} else {
					a.wait = a.waitList()[idx]
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
			d.Invalidate()
		case a.bgListR.Contains(ev.X, ev.Y):
			box := a.bgListR.Inset(2)
			row := (ev.Y - box.Y - 1) / 22
			idx := a.bgScroll.Pos + row
			if idx >= 0 && idx < len(backgroundStyles) && ev.Y < box.Bottom() {
				a.bg = idx
				d.Invalidate()
			}
		case a.styleBoxR.Contains(ev.X, ev.Y):
			a.comboOpen = 1
			d.Invalidate()
		case a.waitBoxR.Contains(ev.X, ev.Y):
			a.comboOpen = 2
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
	a.d.saverStyle = a.style
	a.d.saverWait = time.Duration(a.wait) * time.Minute
	a.d.savePrefs()
	a.d.Invalidate()
}

func (a *DisplayApp) restore() {
	a.d.background = a.origBg
	a.d.saverStyle = a.origStyle
	a.d.saverWait = time.Duration(a.origWait) * time.Minute
	a.d.Invalidate()
}

func (a *DisplayApp) preview() {
	if a.style == 0 {
		a.d.MessageBox("预览", "请先选择一种屏幕保护程序。", IconInfo, []string{"确定"}, nil)
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
			if a.comboOpen == 1 {
				a.style = (a.style + len(saverNames) + delta) % len(saverNames)
			} else {
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
			}
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
		a.tab = 1 - a.tab
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
