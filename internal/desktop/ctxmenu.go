package desktop

import (
	"win95linux/internal/ui"
)

// ctxItem is one row in a popup context menu. A menu has at most one nested
// submenu level, which is all the desktop shell needs.
type ctxItem struct {
	label    string
	accel    string
	icon     ui.IconKind
	sep      bool
	checked  bool
	enabled  bool
	action   func()
	children []ctxItem
}

type contextMenu struct {
	items        []ctxItem
	rect         ui.Rect
	itemRects    []ui.Rect
	hover        int
	sub          int
	subRect      ui.Rect
	subItemRects []ui.Rect
	subHover     int
}

func ctxIconWidth(items []ctxItem) int {
	for _, it := range items {
		if !it.sep && (it.icon != ui.IconNone || it.checked) {
			return 22
		}
	}
	return 0
}

func measureCtxMenu(items []ctxItem) (w, h int) {
	f := ui.DefaultFont()
	iconW := ctxIconWidth(items)
	for _, it := range items {
		if it.sep {
			continue
		}
		iw := 26 + iconW + f.Width(it.label)
		if it.accel != "" {
			iw += 20 + f.Width(it.accel)
		}
		if len(it.children) > 0 {
			iw += 16
		}
		if iw > w {
			w = iw
		}
	}
	if w < 170 {
		w = 170
	}
	if w > 380 {
		w = 380
	}
	h = 8
	for _, it := range items {
		if it.sep {
			h += 9
		} else {
			h += 23
		}
	}
	h += 4
	return w, h
}

func (d *Desktop) openContext(items []ctxItem, x, y int) {
	if len(items) == 0 {
		return
	}
	w, h := measureCtxMenu(items)
	if x+w > d.W-4 {
		x = d.W - 4 - w
	}
	if x < 2 {
		x = 2
	}
	if y+h > d.taskbarTop()-2 {
		y = d.taskbarTop() - 2 - h
	}
	if y < 2 {
		y = 2
	}
	m := &contextMenu{items: items, rect: ui.Rect{x, y, w, h}, hover: -1, sub: -1, subHover: -1}
	m.itemRects = make([]ui.Rect, len(items))
	yy := y + 6
	for i, it := range items {
		if it.sep {
			m.itemRects[i] = ui.Rect{x + 6, yy, w - 12, 9}
			yy += 9
		} else {
			m.itemRects[i] = ui.Rect{x + 3, yy, w - 6, 22}
			yy += 23
		}
	}
	d.ctx = m
	d.startOpen = false
	d.startSub = ""
	d.startSel = ""
	d.startHover = ""
	d.Invalidate()
}

func (d *Desktop) closeContext() {
	if d.ctx != nil {
		d.ctx = nil
		d.Invalidate()
	}
}

func (m *contextMenu) layoutSub(d *Desktop) {
	m.subRect = ui.Rect{}
	m.subItemRects = nil
	if m.sub < 0 || m.sub >= len(m.items) {
		return
	}
	children := m.items[m.sub].children
	if len(children) == 0 {
		m.sub = -1
		return
	}
	w, h := measureCtxMenu(children)
	anchor := m.itemRects[m.sub]
	x := m.rect.Right() - 4
	if x+w > d.W-4 {
		x = m.rect.X - w + 4
	}
	if x < 2 {
		x = 2
	}
	y := anchor.Y - 4
	if y+h > d.taskbarTop()-2 {
		y = d.taskbarTop() - 2 - h
	}
	if y < 2 {
		y = 2
	}
	m.subRect = ui.Rect{x, y, w, h}
	m.subItemRects = make([]ui.Rect, len(children))
	yy := y + 6
	for i, it := range children {
		if it.sep {
			m.subItemRects[i] = ui.Rect{x + 6, yy, w - 12, 9}
			yy += 9
		} else {
			m.subItemRects[i] = ui.Rect{x + 3, yy, w - 6, 22}
			yy += 23
		}
	}
}

func (m *contextMenu) mainIndex(x, y int) int {
	for i, it := range m.items {
		if !it.sep && m.itemRects[i].Contains(x, y) {
			return i
		}
	}
	return -1
}

func (m *contextMenu) subIndex(x, y int) int {
	if m.sub < 0 || m.sub >= len(m.items) {
		return -1
	}
	items := m.items[m.sub].children
	for i, it := range items {
		if !it.sep && m.subItemRects[i].Contains(x, y) {
			return i
		}
	}
	return -1
}

func (m *contextMenu) contains(x, y int) bool {
	if m.rect.Contains(x, y) {
		return true
	}
	return m.sub >= 0 && m.subRect.Contains(x, y)
}

func (d *Desktop) activateCtxItem(it ctxItem) {
	d.ctx = nil
	d.Invalidate()
	if it.enabled && it.action != nil {
		it.action()
	}
}

// ctxMouse handles all mouse input while a popup menu is open. It returns
// false only for a right-click outside the menu, which opens the menu for the
// new target.
func (d *Desktop) ctxMouse(ev MouseEvent) bool {
	m := d.ctx
	if m == nil {
		return false
	}
	x, y := ev.X, ev.Y
	switch ev.Kind {
	case MouseWheel:
		return true
	case MouseMove:
		changed := false
		if m.sub >= 0 && m.subRect.Contains(x, y) {
			if i := m.subIndex(x, y); i >= 0 && i != m.subHover {
				m.subHover = i
				changed = true
			}
			if changed {
				d.Invalidate()
			}
			return true
		}
		if i := m.mainIndex(x, y); i >= 0 {
			if m.hover != i {
				m.hover = i
				changed = true
			}
			it := m.items[i]
			if it.enabled && len(it.children) > 0 {
				if m.sub != i {
					m.sub = i
					m.subHover = -1
					m.layoutSub(d)
					changed = true
				}
			} else if m.sub >= 0 {
				m.sub = -1
				m.subRect = ui.Rect{}
				m.subItemRects = nil
				changed = true
			}
			if changed {
				d.Invalidate()
			}
			return true
		}
		if m.sub >= 0 {
			m.sub = -1
			m.subRect = ui.Rect{}
			m.subItemRects = nil
			changed = true
		}
		if m.hover != -1 {
			m.hover = -1
			changed = true
		}
		if changed {
			d.Invalidate()
		}
		return true
	case MouseDown:
		if ev.Button == 3 {
			if m.contains(x, y) {
				return true
			}
			d.ctx = nil
			d.Invalidate()
			return false
		}
		if ev.Button != 1 {
			return true
		}
		if m.sub >= 0 && m.subRect.Contains(x, y) {
			if i := m.subIndex(x, y); i >= 0 {
				it := m.items[m.sub].children[i]
				if it.enabled {
					d.activateCtxItem(it)
				}
			}
			return true
		}
		if i := m.mainIndex(x, y); i >= 0 {
			it := m.items[i]
			if it.enabled {
				if len(it.children) > 0 {
					m.sub = i
					m.hover = i
					m.subHover = -1
					m.layoutSub(d)
					d.Invalidate()
				} else {
					d.activateCtxItem(it)
				}
			}
			return true
		}
		if m.contains(x, y) {
			return true
		}
		d.ctx = nil
		d.Invalidate()
		return true
	case MouseUp:
		return true
	}
	return true
}

func nextCtxIndex(items []ctxItem, from, dir int) int {
	n := len(items)
	if n == 0 {
		return -1
	}
	i := from
	if from < 0 {
		if dir > 0 {
			i = -1
		} else {
			i = n
		}
	}
	for k := 0; k < n; k++ {
		i = (i + dir + n) % n
		if !items[i].sep && items[i].enabled {
			return i
		}
	}
	return -1
}

func (d *Desktop) ctxKey(ev KeyEvent) bool {
	m := d.ctx
	if m == nil {
		return false
	}
	switch ev.Name {
	case "Esc":
		if m.sub >= 0 {
			m.sub = -1
			m.subRect = ui.Rect{}
			m.subItemRects = nil
		} else {
			d.ctx = nil
		}
	case "Up", "Down":
		dir := 1
		if ev.Name == "Up" {
			dir = -1
		}
		if m.sub >= 0 {
			m.subHover = nextCtxIndex(m.items[m.sub].children, m.subHover, dir)
		} else {
			m.hover = nextCtxIndex(m.items, m.hover, dir)
		}
	case "Right":
		if m.sub < 0 && m.hover >= 0 && m.hover < len(m.items) {
			it := m.items[m.hover]
			if it.enabled && len(it.children) > 0 {
				m.sub = m.hover
				m.layoutSub(d)
				m.subHover = nextCtxIndex(it.children, -1, 1)
			}
		}
	case "Left":
		if m.sub >= 0 {
			m.sub = -1
			m.subRect = ui.Rect{}
			m.subItemRects = nil
		} else {
			d.ctx = nil
		}
	case "Enter":
		if m.sub >= 0 && m.subHover >= 0 && m.subHover < len(m.items[m.sub].children) {
			d.activateCtxItem(m.items[m.sub].children[m.subHover])
		} else if m.hover >= 0 && m.hover < len(m.items) {
			it := m.items[m.hover]
			if it.enabled {
				if len(it.children) > 0 {
					m.sub = m.hover
					m.layoutSub(d)
					m.subHover = nextCtxIndex(it.children, -1, 1)
				} else {
					d.activateCtxItem(it)
				}
			}
		}
	}
	d.Invalidate()
	return true
}

func drawCtxMenuBox(c *ui.Canvas, items []ctxItem, rect ui.Rect, itemRects []ui.Rect, hover int) {
	c.FillRect(rect, ui.ColFace)
	c.Raised(rect)
	f := ui.DefaultFont()
	iconW := ctxIconWidth(items)
	for i, it := range items {
		r := itemRects[i]
		if it.sep {
			c.HLine(rect.X+8, r.Y+3, rect.W-16, ui.ColShadow)
			c.HLine(rect.X+8, r.Y+4, rect.W-16, ui.ColWhite)
			continue
		}
		hov := i == hover && it.enabled
		if hov {
			c.FillRect(r, ui.ColNavy)
		}
		col := ui.ColText
		if !it.enabled {
			col = ui.ColTextDis
		}
		if hov {
			col = ui.ColWhite
		}
		tx := r.X + 8
		if iconW > 0 {
			if it.checked {
				ui.CheckMark(c, r.X+5, r.Y+5, col)
			} else if it.icon != ui.IconNone {
				ui.DrawIcon16(c, it.icon, r.X+4, r.Y+3)
			}
			tx = r.X + 27
		} else if it.checked {
			ui.CheckMark(c, r.X+5, r.Y+5, col)
			tx = r.X + 26
		}
		f.Text(c, tx, r.Y+3, it.label, col)
		if it.accel != "" {
			acol := ui.ColShadow
			if hov {
				acol = ui.ColWhite
			}
			f.TextRight(c, r.Right()-9, r.Y+3, it.accel, acol)
		}
		if len(it.children) > 0 {
			acol := ui.ColText
			if !it.enabled {
				acol = ui.ColTextDis
			}
			if hov {
				acol = ui.ColWhite
			}
			ui.Arrow(c, r.Right()-13, r.Y+7, "right", acol)
		}
	}
}

func (d *Desktop) drawContext(c *ui.Canvas) {
	m := d.ctx
	if m == nil {
		return
	}
	drawCtxMenuBox(c, m.items, m.rect, m.itemRects, m.hover)
	if m.sub >= 0 && m.sub < len(m.items) && len(m.subItemRects) > 0 {
		drawCtxMenuBox(c, m.items[m.sub].children, m.subRect, m.subItemRects, m.subHover)
	}
}
