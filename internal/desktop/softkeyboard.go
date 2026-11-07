package desktop

import (
	"strings"

	"win95linux/internal/ui"
)

type softKey struct {
	label      string
	shiftLabel string
	units      float64
	name       string
	rune       rune
	shiftRune  rune
	kind       string
}

type softKeyboard struct {
	shift    bool
	ctrl     bool
	alt      bool
	caps     bool
	hover    int
	press    int
	captured bool
}

type softKeyGeom struct {
	rect  ui.Rect
	key   softKey
	index int
}

type softKBLayout struct {
	outer ui.Rect
	title ui.Rect
	close ui.Rect
	keys  []softKeyGeom
}

func letterKey(lower rune) softKey {
	up := lower - 'a' + 'A'
	return softKey{
		label: string(lower), shiftLabel: string(up), units: 1,
		name: string(up), rune: lower, shiftRune: up,
	}
}

func charKey(label, shiftLabel string, r, sr rune) softKey {
	return softKey{label: label, shiftLabel: shiftLabel, units: 1, rune: r, shiftRune: sr}
}

func softKeyboardRows() [][]softKey {
	return [][]softKey{
		{
			{label: "Esc", units: 1.4, name: "Esc"},
			charKey("1", "!", '1', '!'), charKey("2", "@", '2', '@'), charKey("3", "#", '3', '#'),
			charKey("4", "$", '4', '$'), charKey("5", "%", '5', '%'), charKey("6", "^", '6', '^'),
			charKey("7", "&", '7', '&'), charKey("8", "*", '8', '*'), charKey("9", "(", '9', '('),
			charKey("0", ")", '0', ')'), charKey("-", "_", '-', '_'), charKey("=", "+", '=', '+'),
			{label: "Bksp", units: 2, name: "Backspace"},
		},
		{
			{label: "Tab", units: 1.5, name: "Tab"},
			letterKey('q'), letterKey('w'), letterKey('e'), letterKey('r'), letterKey('t'),
			letterKey('y'), letterKey('u'), letterKey('i'), letterKey('o'), letterKey('p'),
			charKey("[", "{", '[', '{'), charKey("]", "}", ']', '}'),
			charKey("\\", "|", '\\', '|'),
		},
		{
			{label: "Caps", units: 1.8, kind: "caps"},
			letterKey('a'), letterKey('s'), letterKey('d'), letterKey('f'), letterKey('g'),
			letterKey('h'), letterKey('j'), letterKey('k'), letterKey('l'),
			charKey(";", ":", ';', ':'), charKey("'", "\"", '\'', '"'),
			{label: "Enter", units: 2.2, name: "Enter"},
		},
		{
			{label: "Shift", units: 2.4, kind: "shift"},
			letterKey('z'), letterKey('x'), letterKey('c'), letterKey('v'), letterKey('b'),
			letterKey('n'), letterKey('m'),
			charKey(",", "<", ',', '<'), charKey(".", ">", '.', '>'), charKey("/", "?", '/', '?'),
			{label: "Shift", units: 2.4, kind: "shift"},
		},
		{
			{label: "Ctrl", units: 1.5, kind: "ctrl"},
			{label: "Alt", units: 1.5, kind: "alt"},
			{label: "Space", units: 7, name: "Space", rune: ' '},
			{label: "", units: 1, name: "Left", kind: "arrow"},
			{label: "", units: 1, name: "Up", kind: "arrow"},
			{label: "", units: 1, name: "Down", kind: "arrow"},
			{label: "", units: 1, name: "Right", kind: "arrow"},
			{label: "Del", units: 1.3, name: "Delete"},
		},
	}
}

func (d *Desktop) toggleSoftKeyboard() {
	if d.sk != nil {
		d.sk = nil
		d.hand = false
		d.Invalidate()
		return
	}
	d.sk = &softKeyboard{hover: -1, press: -1}
	d.startOpen = false
	d.startSub = ""
	if d.ctx != nil {
		d.ctx = nil
	}
	d.Invalidate()
}

func (d *Desktop) softKeyboardLayout() softKBLayout {
	rows := softKeyboardRows()
	const titleH = 22
	const rowH = 34
	const gap = 4
	const pad = 8
	h := titleH + pad + len(rows)*rowH + (len(rows)-1)*gap + pad
	w := 700
	if d.W-24 < w {
		w = d.W - 24
	}
	if w < 360 {
		w = 360
	}
	x := (d.W - w) / 2
	y := d.taskbarTop() - h - 8
	if y < 4 {
		y = 4
	}
	outer := ui.Rect{x, y, w, h}
	lay := softKBLayout{
		outer: outer,
		title: ui.Rect{outer.X + 2, outer.Y + 2, outer.W - 4, titleH},
		close: ui.Rect{outer.Right() - 19, outer.Y + 5, 14, 14},
	}
	maxUnits := 15.4
	gaps := 13.0
	contentW := float64(outer.W - 2*pad)
	unit := (contentW - gaps*gap) / maxUnits
	if unit > 46 {
		unit = 46
	}
	if unit < 18 {
		unit = 18
	}
	y = outer.Y + titleH + pad
	for ri, row := range rows {
		rowW := 0
		for i, k := range row {
			rowW += int(k.units * unit)
			if i > 0 {
				rowW += gap
			}
		}
		x := outer.X + (outer.W-rowW)/2
		for i, k := range row {
			kw := int(k.units * unit)
			lay.keys = append(lay.keys, softKeyGeom{
				rect:  ui.Rect{x, y, kw, rowH},
				key:   k,
				index: ri*100 + i,
			})
			x += kw + gap
		}
		y += rowH + gap
	}
	return lay
}

func (d *Desktop) softKeyAt(lay softKBLayout, x, y int) int {
	for _, kg := range lay.keys {
		if kg.rect.Contains(x, y) {
			return kg.index
		}
	}
	return -1
}

func (d *Desktop) softKeyByIndex(lay softKBLayout, idx int) (softKey, bool) {
	for _, kg := range lay.keys {
		if kg.index == idx {
			return kg.key, true
		}
	}
	return softKey{}, false
}

func (d *Desktop) softKBMouse(ev MouseEvent) bool {
	if d.sk == nil {
		return false
	}
	lay := d.softKeyboardLayout()
	inside := lay.outer.Contains(ev.X, ev.Y)
	overClose := lay.close.Contains(ev.X, ev.Y)
	switch ev.Kind {
	case MouseMove:
		if inside {
			hover := d.softKeyAt(lay, ev.X, ev.Y)
			if hover != d.sk.hover {
				d.sk.hover = hover
				d.Invalidate()
			}
			d.hand = hover >= 0 || overClose
			return true
		}
		if d.sk.hover != -1 {
			d.sk.hover = -1
			d.Invalidate()
		}
		return false
	case MouseDown:
		if ev.Button != 1 || !inside {
			return false
		}
		d.sk.captured = true
		d.sk.press = d.softKeyAt(lay, ev.X, ev.Y)
		if overClose {
			d.sk.press = -2
		}
		d.hand = true
		d.Invalidate()
		return true
	case MouseUp:
		if !d.sk.captured || ev.Button != 1 {
			return false
		}
		d.sk.captured = false
		press := d.sk.press
		d.sk.press = -1
		if inside && press == -2 && overClose {
			d.toggleSoftKeyboard()
			return true
		}
		if inside {
			if idx := d.softKeyAt(lay, ev.X, ev.Y); idx >= 0 && idx == press {
				if k, ok := d.softKeyByIndex(lay, idx); ok {
					d.activateSoftKey(k)
				}
			}
		}
		d.Invalidate()
		return true
	}
	return false
}

func (d *Desktop) clearSoftModifiers() {
	if d.sk == nil {
		return
	}
	d.sk.shift = false
	d.sk.ctrl = false
	d.sk.alt = false
}

func (d *Desktop) activateSoftKey(k softKey) {
	if d.sk == nil {
		return
	}
	switch k.kind {
	case "shift":
		d.sk.shift = !d.sk.shift
		d.Invalidate()
		return
	case "ctrl":
		d.sk.ctrl = !d.sk.ctrl
		d.Invalidate()
		return
	case "alt":
		d.sk.alt = !d.sk.alt
		d.Invalidate()
		return
	case "caps":
		d.sk.caps = !d.sk.caps
		d.Invalidate()
		return
	}
	r := k.rune
	if k.rune >= 'a' && k.rune <= 'z' {
		if d.sk.shift != d.sk.caps {
			r = k.shiftRune
		}
	} else if d.sk.shift && k.shiftRune != 0 {
		r = k.shiftRune
	}
	name := k.name
	ev := KeyEvent{Rune: r, Name: name, Shift: d.sk.shift, Ctrl: d.sk.ctrl, Alt: d.sk.alt}
	d.clearSoftModifiers()
	d.Key(ev)
	d.Invalidate()
}

func (d *Desktop) softKeyLabel(k softKey) string {
	if k.rune >= 'a' && k.rune <= 'z' {
		if d.sk.shift != d.sk.caps {
			return k.shiftLabel
		}
		return k.label
	}
	if d.sk.shift && k.shiftLabel != "" {
		return k.shiftLabel
	}
	return k.label
}

func (d *Desktop) drawSoftKeyboard(c *ui.Canvas) {
	if d.sk == nil {
		return
	}
	lay := d.softKeyboardLayout()
	c.FillRect(lay.outer, ui.ColFace)
	c.Raised(lay.outer)
	c.TitleBar(lay.title, ui.ColNavy, ui.ColNavyLight)
	f := ui.DefaultFont()
	f.Text(c, lay.title.X+6, lay.title.Y+4, "屏幕键盘", ui.ColWhite)
	hint := "Ctrl+Alt+K 关闭"
	f.TextRight(c, lay.close.X-6, lay.title.Y+4, hint, 0xC0E0FF)
	ui.SmallButton(c, lay.close, "×", d.sk.press == -2)

	for _, kg := range lay.keys {
		k := kg.key
		pressed := d.sk.press == kg.index
		active := false
		switch k.kind {
		case "shift":
			active = d.sk.shift
		case "ctrl":
			active = d.sk.ctrl
		case "alt":
			active = d.sk.alt
		case "caps":
			active = d.sk.caps
		}
		ui.Button(c, kg.rect, "", pressed || active, true)
		if k.kind == "arrow" {
			dir := strings.ToLower(k.name)
			ax := kg.rect.X + kg.rect.W/2 - 4
			ay := kg.rect.Y + kg.rect.H/2 - 4
			ui.Arrow(c, ax, ay, dir, ui.ColText)
			continue
		}
		label := d.softKeyLabel(k)
		tx := kg.rect.X + (kg.rect.W-f.Width(label))/2
		ty := kg.rect.Y + (kg.rect.H-f.Height())/2
		if pressed || active {
			tx++
			ty++
		}
		f.Text(c, tx, ty, label, ui.ColText)
	}
}
