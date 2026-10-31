package desktop

import (
	"time"

	"win95linux/internal/ui"
)

func (d *Desktop) Mouse(ev MouseEvent) {
	if d.bsod {
		d.handleBSODInput()
		return
	}
	if d.sleepPending || d.sleeping {
		return
	}
	if d.saver {
		d.stopSaver(time.Now())
		return
	}
	d.lastInput = time.Now()
	gx, gy := ev.X, ev.Y
	d.MouseX, d.MouseY = gx, gy
	d.hand = false

	if ev.Kind == MouseDown && ev.Button == 1 && len(d.notices) > 0 && d.noticeRect.Contains(gx, gy) {
		d.dismissNotice()
		return
	}

	if d.ctx != nil && d.ctxMouse(ev) {
		return
	}

	if ev.Kind == MouseWheel {
		if d.modal != nil {
			d.routeWheel(d.modal, gx, gy, ev)
			return
		}
		if d.startOpen {
			return
		}
		if w := d.windowAt(gx, gy); w != nil {
			d.routeWheel(w, gx, gy, ev)
			return
		}
		return
	}

	if d.modal != nil {
		d.mouseWindow(d.modal, ev, true)
		return
	}

	if d.startOpen {
		d.mouseStart(ev)
		return
	}

	if ev.Kind == MouseMove {
		d.mouseMove(ev)
		return
	}

	if ev.Kind == MouseDown {
		if ev.Button == 3 {
			d.rightClick(gx, gy)
			return
		}
		if ev.Button != 1 {
			return
		}
		d.mouseDown = true
		if gy >= d.taskbarTop() {
			d.mouseTaskbar(gx, gy)
			return
		}
		if d.active != nil && d.active.menuOpen >= 0 {
			if d.mouseMenu(d.active, gx, gy) {
				return
			}
			d.active.menuOpen = -1
			return
		}
		if idx := d.iconAt(gx, gy); idx >= 0 {
			if idx == d.lastIcon && time.Since(d.lastClick) < 450*time.Millisecond {
				d.lastIcon = -1
				d.lastClick = time.Time{}
				d.dragIcon = -1
				d.activateIcon(idx)
				return
			}
			d.selected = idx
			d.lastIcon = idx
			d.lastClick = time.Now()
			if !d.autoArrange {
				ix, iy := d.iconPos(idx)
				d.dragIcon = idx
				d.dragIconDX = gx - ix
				d.dragIconDY = gy - iy
				d.dragStartX, d.dragStartY = gx, gy
				d.dragIconMoved = false
			} else {
				d.dragIcon = -1
			}
			d.Invalidate()
			return
		}
		d.selected = -1
		if w := d.windowAt(gx, gy); w != nil {
			d.mouseWindow(w, ev, false)
			return
		}
		d.active = nil
		d.Invalidate()
		return
	}

	if ev.Kind == MouseUp {
		if ev.Button != 1 {
			d.mouseDown = false
			return
		}
		if d.dragIcon >= 0 {
			d.dragIcon = -1
			d.dragIconMoved = false
			d.Invalidate()
		}
		d.mouseUp(ev)
	}
}

func (d *Desktop) routeWheel(w *Window, gx, gy int, ev MouseEvent) {
	if w == nil {
		return
	}
	client := w.ClientRect()
	if !client.Contains(gx, gy) {
		return
	}
	ev.Button = 0
	w.App.Mouse(d, w, client, ev)
	d.Invalidate()
}

func (d *Desktop) mouseMove(ev MouseEvent) {
	if d.dragIcon >= 0 && d.mouseDown && d.dragIcon < len(d.icons) {
		if !d.dragIconMoved {
			if absInt(ev.X-d.dragStartX)+absInt(ev.Y-d.dragStartY) < 4 {
				return
			}
			d.dragIconMoved = true
			d.lastIcon = -1
			d.lastClick = time.Time{}
		}
		nx := ev.X - d.dragIconDX
		ny := ev.Y - d.dragIconDY
		if nx < 0 {
			nx = 0
		}
		if nx > d.W-88 {
			nx = d.W - 88
		}
		if ny < 0 {
			ny = 0
		}
		if ny > d.taskbarTop()-78 {
			ny = d.taskbarTop() - 78
		}
		d.iconPositions[d.icons[d.dragIcon].key] = iconPoint{nx, ny}
		d.Invalidate()
		return
	}
	for _, w := range d.windows {
		if w.drag {
			nx := ev.X - w.dragDX
			ny := ev.Y - w.dragDY
			if nx > d.W-80 {
				nx = d.W - 80
			}
			if nx < -w.W+80 {
				nx = -w.W + 80
			}
			if ny < 0 {
				ny = 0
			}
			if ny > d.taskbarTop()-24 {
				ny = d.taskbarTop() - 24
			}
			w.X, w.Y = nx, ny
			d.Invalidate()
			return
		}
		if w.resizing {
			dx := ev.X - w.resizeX
			dy := ev.Y - w.resizeY
			nw := w.restore.W + dx
			nh := w.restore.H + dy
			if nw < w.MinW {
				nw = w.MinW
			}
			if nh < w.MinH {
				nh = w.MinH
			}
			if w.X+nw > d.W {
				nw = d.W - w.X
			}
			if w.Y+nh > d.taskbarTop() {
				nh = d.taskbarTop() - w.Y
			}
			w.W, w.H = nw, nh
			w.App.Resized(d, w, w.ClientRect())
			d.Invalidate()
			return
		}
	}

	if w := d.active; w != nil && w.menuOpen >= 0 && !w.Minimized {
		d.refreshMenus(w)
		if w.menuOpen < len(w.menus) {
			if idx := d.menuItemAt(w, ev.X, ev.Y); idx >= 0 {
				w.menuHover = idx
				return
			}
		}
	}

	if d.mouseDown && d.capture != nil {
		w := d.capture
		client := w.ClientRect()
		w.App.Mouse(d, w, client, ev)
		return
	}

	if w := d.windowAt(ev.X, ev.Y); w != nil {
		if client := w.ClientRect(); client.Contains(ev.X, ev.Y) {
			if d.active != w {
				// Hovering must not steal focus, only reflect in painted state.
			}
			w.App.Mouse(d, w, client, ev)
		}
	}
}

func (d *Desktop) mouseUp(ev MouseEvent) {
	d.mouseDown = false
	for _, w := range d.windows {
		if w.drag {
			w.drag = false
		}
		if w.resizing {
			w.resizing = false
			w.App.Resized(d, w, w.ClientRect())
		}
		if w.pressedBtn != "" {
			btn := w.pressedBtn
			w.pressedBtn = ""
			minR, maxR, closeR := d.titleButtons(w)
			switch btn {
			case "min":
				if minR.Contains(ev.X, ev.Y) {
					d.minimize(w)
				}
			case "max":
				if maxR.Contains(ev.X, ev.Y) {
					d.toggleMax(w)
				}
			case "close":
				if closeR.Contains(ev.X, ev.Y) {
					d.CloseWindow(w)
				}
			}
			d.Invalidate()
			if d.capture == nil {
				return
			}
		}
	}

	if d.modal != nil {
		if d.modal.Minimized {
			d.modal.Minimized = false
		}
		w := d.modal
		client := w.ClientRect()
		if client.Contains(ev.X, ev.Y) || d.capture == w {
			w.App.Mouse(d, w, client, ev)
		}
		d.capture = nil
		d.Invalidate()
		return
	}

	if d.capture != nil {
		w := d.capture
		client := w.ClientRect()
		w.App.Mouse(d, w, client, ev)
		d.capture = nil
	}
	d.Invalidate()
}

func (d *Desktop) windowAt(x, y int) *Window {
	for i := len(d.windows) - 1; i >= 0; i-- {
		w := d.windows[i]
		if w.Minimized {
			continue
		}
		if w.Outer().Contains(x, y) {
			return w
		}
	}
	return nil
}

func (d *Desktop) mouseWindow(w *Window, ev MouseEvent, modal bool) {
	gx, gy := ev.X, ev.Y
	if ev.Kind == MouseDown {
		d.bringToFront(w)
		if !modal {
			d.closeMenusExcept(w)
		}
		if modal {
			d.modal = w
		}
	}
	if !w.Outer().Contains(gx, gy) && d.capture != w {
		return
	}
	minR, maxR, closeR := d.titleButtons(w)
	if ev.Kind == MouseDown && ev.Button == 1 {
		switch {
		case closeR.Contains(gx, gy):
			w.pressedBtn = "close"
			return
		case w.Resizable && maxR.Contains(gx, gy):
			w.pressedBtn = "max"
			return
		case minR.Contains(gx, gy):
			w.pressedBtn = "min"
			return
		}
		if len(w.menus) > 0 {
			mb := w.MenuBarRect()
			if mb.Contains(gx, gy) {
				d.refreshMenus(w)
				f := ui.DefaultFont()
				x := mb.X + 4
				idx := -1
				for i, m := range w.menus {
					lw := f.Width(m.Label) + 14
					if gx >= x && gx < x+lw {
						idx = i
						break
					}
					x += lw
				}
				if idx >= 0 {
					if w.menuOpen == idx {
						w.menuOpen = -1
					} else {
						w.menuOpen = idx
						w.menuHover = -1
					}
				}
				return
			}
			if w.menuOpen >= 0 {
				if d.mouseMenu(w, gx, gy) {
					return
				}
				w.menuOpen = -1
				return
			}
		}
		if w.Resizable && !w.Maximized {
			grip := ui.Rect{w.X + w.W - 18, w.Y + w.H - 18, 18, 18}
			if grip.Contains(gx, gy) {
				w.resizing = true
				w.resizeX, w.resizeY = gx, gy
				w.restore = ui.Rect{w.X, w.Y, w.W, w.H}
				d.capture = w
				return
			}
		}
		if w.TitleRect().Contains(gx, gy) {
			if time.Since(w.titleClick) < 450*time.Millisecond {
				w.titleClick = time.Time{}
				if w.Resizable {
					d.toggleMax(w)
				}
				return
			}
			w.titleClick = time.Now()
			if !w.Maximized {
				w.drag = true
				w.dragDX = gx - w.X
				w.dragDY = gy - w.Y
				d.capture = w
			}
			return
		}
	}
	client := w.ClientRect()
	if client.Contains(gx, gy) || d.capture == w {
		w.App.Mouse(d, w, client, ev)
		if ev.Kind == MouseDown {
			d.capture = w
		}
	}
}

func (d *Desktop) mouseTaskbar(x, y int) {
	top := d.taskbarTop()
	start := ui.Rect{4, top + 4, startBtnW - 2, 26}
	if start.Contains(x, y) {
		d.startOpen = !d.startOpen
		d.startSub = ""
		d.startSel = ""
		d.startHover = ""
		d.closeMenusExcept(nil)
		d.Invalidate()
		return
	}
	if d.active != nil {
		d.active.menuOpen = -1
	}
	if d.startOpen {
		d.startOpen = false
	}
	clock := d.clockRect()
	if clock.Contains(x, y) {
		d.MessageBox("日期/时间", d.statusClock(), IconInfo, []string{"确定"}, nil)
		return
	}
	if d.sysTrayRect().Contains(x, y) {
		d.showSysMonitor()
		return
	}

	for _, tb := range d.taskbarButtons() {
		if tb.r.Contains(x, y) {
			if tb.w == d.active && !tb.w.Minimized {
				d.minimize(tb.w)
			} else {
				d.bringToFront(tb.w)
			}
			return
		}
	}
}

func (d *Desktop) minimize(w *Window) {
	w.Minimized = true
	w.menuOpen = -1
	if d.active == w {
		d.active = nil
		for i := len(d.windows) - 1; i >= 0; i-- {
			c := d.windows[i]
			if c != w && !c.Minimized && !c.Modal {
				d.active = c
				break
			}
		}
	}
	d.Invalidate()
}

func (d *Desktop) toggleMax(w *Window) {
	if w.Maximized {
		w.X, w.Y, w.W, w.H = w.restore.X, w.restore.Y, w.restore.W, w.restore.H
		w.Maximized = false
	} else {
		w.restore = ui.Rect{w.X, w.Y, w.W, w.H}
		w.X, w.Y = 0, 0
		w.W, w.H = d.W, d.taskbarTop()
		w.Maximized = true
	}
	w.App.Resized(d, w, w.ClientRect())
	d.Invalidate()
}

func (d *Desktop) mouseStart(ev MouseEvent) {
	gx, gy := ev.X, ev.Y
	if ev.Kind == MouseMove {
		if sub, id := d.startSubAt(gx, gy); sub != "" {
			d.startHover = id
			return
		}
		if id := d.startItemAt(gx, gy); id != "" {
			d.startSel = id
			e := d.findStart(id)
			if e != nil && len(e.children) > 0 {
				d.startSub = id
			}
			return
		}
		if !d.startMenuRect().Contains(gx, gy) {
			// Keep the submenu open while moving across its gap.
			for _, e := range d.menuEntries() {
				if e.id == d.startSub && len(e.children) > 0 {
					rects := d.startItemRects()
					idx := -1
					for i, m := range d.menuEntries() {
						if m.id == e.id {
							idx = i
						}
					}
					if idx >= 0 {
						sr := d.startSubRect(rects[idx], e)
						if !sr.Contains(gx, gy) {
							d.startSub = ""
						}
					}
				}
			}
		}
		d.startSel = ""
		d.startHover = ""
		return
	}
	if ev.Kind == MouseDown {
		if sub, id := d.startSubAt(gx, gy); sub != "" {
			for _, e := range d.menuEntries() {
				for _, s := range e.children {
					if s.id == id && s.enabled && s.action != nil {
						d.startOpen = false
						d.startSub = ""
						s.action()
						return
					}
				}
			}
			return
		}
		if id := d.startItemAt(gx, gy); id != "" {
			e := d.findStart(id)
			if e == nil {
				return
			}
			if len(e.children) > 0 {
				d.startSub = id
				d.startSel = id
				return
			}
			d.startOpen = false
			d.startSub = ""
			if e.enabled && e.action != nil {
				e.action()
			}
			return
		}
		// Clicking outside closes the menu.
		d.startOpen = false
		d.startSub = ""
		return
	}
}

func (d *Desktop) findStart(id string) *startEntry {
	for i, e := range d.menuEntries() {
		if e.id == id {
			entries := d.menuEntries()
			return &entries[i]
		}
	}
	return nil
}

func (d *Desktop) menuRectFor(w *Window, idx int) ui.Rect {
	mb := w.MenuBarRect()
	f := ui.DefaultFont()
	x := mb.X + 4
	for i := 0; i < idx && i < len(w.menus); i++ {
		x += f.Width(w.menus[i].Label) + 14
	}
	width := 160
	for _, it := range w.menus[idx].Items {
		iwidth := 44 + f.Width(it.Label) + f.Width(it.Accel)
		if it.Separator {
			continue
		}
		if iwidth > width {
			width = iwidth
		}
	}
	if width > 320 {
		width = 320
	}
	h := 6
	for _, it := range w.menus[idx].Items {
		if it.Separator {
			h += 7
		} else {
			h += 22
		}
	}
	h += 4
	r := ui.Rect{x, mb.Bottom(), width, h}
	if r.Bottom() > d.taskbarTop()-2 {
		r.Y = mb.Y - h
	}
	return r
}

func (d *Desktop) drawMenuDropdown(c *ui.Canvas, w *Window) {
	if w.menuOpen < 0 || w.menuOpen >= len(w.menus) {
		return
	}
	r := d.menuRectFor(w, w.menuOpen)
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	f := ui.DefaultFont()
	y := r.Y + 4
	for i, it := range w.menus[w.menuOpen].Items {
		if it.Separator {
			c.HLine(r.X+6, y+3, r.W-12, ui.ColShadow)
			c.HLine(r.X+6, y+4, r.W-12, ui.ColWhite)
			y += 7
			continue
		}
		ir := ui.Rect{r.X + 3, y, r.W - 6, 21}
		hover := i == w.menuHover && it.Enabled
		if hover {
			c.FillRect(ir, ui.ColNavy)
		}
		if it.Checked {
			ui.CheckMark(c, ir.X+4, ir.Y+4, ui.ColDark)
		}
		col := ui.ColText
		if !it.Enabled {
			col = ui.ColTextDis
		}
		if hover {
			col = ui.ColWhite
		}
		f.Text(c, ir.X+25, ir.Y+3, it.Label, col)
		if it.Accel != "" {
			acol := ui.ColShadow
			if hover {
				acol = ui.ColWhite
			}
			f.TextRight(c, ir.Right()-8, ir.Y+3, it.Accel, acol)
		}
		y += 22
	}
}

func (d *Desktop) menuItemAt(w *Window, x, y int) int {
	if w.menuOpen < 0 || w.menuOpen >= len(w.menus) {
		return -1
	}
	r := d.menuRectFor(w, w.menuOpen)
	if !r.Contains(x, y) {
		return -1
	}
	yy := r.Y + 4
	for i, it := range w.menus[w.menuOpen].Items {
		if it.Separator {
			yy += 7
			continue
		}
		if y >= yy && y < yy+21 {
			return i
		}
		yy += 22
	}
	return -1
}

// mouseMenu returns true when the click was consumed by an open window menu.
func (d *Desktop) mouseMenu(w *Window, x, y int) bool {
	if w.menuOpen < 0 || w.menuOpen >= len(w.menus) {
		return false
	}
	if r := d.menuRectFor(w, w.menuOpen); r.Contains(x, y) {
		idx := d.menuItemAt(w, x, y)
		if idx >= 0 {
			it := w.menus[w.menuOpen].Items[idx]
			w.menuOpen = -1
			if it.Enabled && it.Action != nil {
				it.Action()
			}
		}
		return true
	}
	if mb := w.MenuBarRect(); mb.Contains(x, y) {
		f := ui.DefaultFont()
		xx := mb.X + 4
		for i, m := range w.menus {
			lw := f.Width(m.Label) + 14
			if x >= xx && x < xx+lw {
				w.menuOpen = i
				w.menuHover = -1
				return true
			}
			xx += lw
		}
	}
	return false
}

func (d *Desktop) Key(ev KeyEvent) {
	if d.bsod {
		d.handleBSODInput()
		return
	}
	if d.sleepPending || d.sleeping {
		return
	}
	if ev.Name == "Print" {
		d.takeScreenshot()
		if d.saver {
			d.stopSaver(time.Now())
		}
		return
	}
	if d.saver {
		d.stopSaver(time.Now())
		return
	}
	d.lastInput = time.Now()
	if d.ctx != nil {
		d.ctxKey(ev)
		return
	}
	if ev.Name == "Esc" {
		if d.startOpen {
			d.startOpen = false
			d.startSub = ""
			return
		}
		if d.modal == nil && d.active != nil && d.active.menuOpen >= 0 {
			d.active.menuOpen = -1
			return
		}
	}
	if d.modal == nil && ev.Alt && ev.Name == "Tab" {
		d.cycleWindows()
		return
	}
	if d.modal == nil && ev.Ctrl && ev.Name == "Esc" {
		d.startOpen = !d.startOpen
		d.startSub = ""
		return
	}
	if ev.Alt && ev.Name == "F4" {
		if d.modal != nil {
			d.CloseWindow(d.modal)
			return
		}
		if d.active != nil && !d.active.Minimized {
			d.CloseWindow(d.active)
			return
		}
	}
	if d.modal == nil && ev.Alt && ev.Name == " " && d.active != nil && !d.active.Minimized {
		w := d.active
		d.openContext(d.windowMenuItems(w), w.X+8, w.Y+titleH+8)
		return
	}
	if d.modal != nil {
		w := d.modal
		if client := w.ClientRect(); client.W > 0 {
			w.App.Key(d, w, ev)
		}
		d.Invalidate()
		return
	}
	if d.active == nil || d.active.Minimized {
		d.desktopKey(ev)
		return
	}
	w := d.active
	if w.menuOpen >= 0 {
		d.refreshMenus(w)
		if w.menuOpen < len(w.menus) {
			items := w.menus[w.menuOpen].Items
			switch ev.Name {
			case "Esc":
				w.menuOpen = -1
				return
			case "Up":
				d.moveMenuSel(w, -1)
				return
			case "Down":
				d.moveMenuSel(w, 1)
				return
			case "Left":
				w.menuOpen = (w.menuOpen + len(w.menus) - 1) % len(w.menus)
				w.menuHover = -1
				return
			case "Right":
				w.menuOpen = (w.menuOpen + 1) % len(w.menus)
				w.menuHover = -1
				return
			case "Enter":
				if w.menuHover >= 0 && w.menuHover < len(items) {
					it := items[w.menuHover]
					w.menuOpen = -1
					if it.Enabled && it.Action != nil {
						it.Action()
					}
				}
				return
			}
		}
	}
	if w.App.Key(d, w, ev) {
		d.Invalidate()
	}
}

func (d *Desktop) moveMenuSel(w *Window, dir int) {
	items := w.menus[w.menuOpen].Items
	n := len(items)
	if n == 0 {
		return
	}
	i := w.menuHover
	for k := 0; k < n; k++ {
		i = (i + dir + n) % n
		if !items[i].Separator && items[i].Enabled {
			w.menuHover = i
			return
		}
	}
}

func (d *Desktop) cycleWindows() {
	var list []*Window
	for _, w := range d.windows {
		if !w.Modal && !w.Minimized {
			list = append(list, w)
		}
	}
	if len(list) < 2 {
		return
	}
	cur := 0
	for i, w := range list {
		if w == d.active {
			cur = i
			break
		}
	}
	next := list[(cur+1)%len(list)]
	d.bringToFront(next)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
