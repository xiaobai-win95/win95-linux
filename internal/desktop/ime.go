package desktop

import (
	"strings"

	"win95linux/internal/ime"
	"win95linux/internal/ui"
)

type pinyinState struct {
	enabled    bool
	owner      *Window
	anchor     ui.Rect
	insert     func(string)
	engine     *ime.Engine
	candidates []ime.Candidate
	rects      []ui.Rect
	window     ui.Rect
}

func (d *Desktop) imeFocus(w *Window, anchor ui.Rect, insert func(string)) {
	if d.ime.owner != w {
		d.ime.engine = nil
		d.ime.candidates = nil
	}
	d.ime.owner = w
	d.ime.anchor = anchor
	d.ime.insert = insert
}

func (d *Desktop) imeCurrent() *Window {
	if d.modal != nil {
		return d.modal
	}
	return d.active
}

func (d *Desktop) imeComposing() bool {
	return d.ime.engine != nil && d.ime.engine.Composition() != ""
}

func (d *Desktop) imeCommit(text string) {
	d.ime.engine = nil
	d.ime.candidates = nil
	d.ime.rects = nil
	d.ime.window = ui.Rect{}
	if text != "" && d.ime.insert != nil {
		d.ime.insert(text)
	}
}

func (d *Desktop) toggleIME() {
	d.ime.enabled = !d.ime.enabled
	if !d.ime.enabled {
		d.imeCommit("")
	}
	d.Invalidate()
}

func (d *Desktop) imeHandle(ev KeyEvent) bool {
	if ev.Ctrl && ev.Shift && !ev.Alt && (ev.Name == "Space" || ev.Rune == ' ') {
		d.toggleIME()
		return true
	}
	w := d.imeCurrent()
	if !d.ime.enabled || w == nil || w.Minimized || d.ime.owner != w || d.ime.insert == nil || ev.Ctrl || ev.Alt {
		return false
	}
	composing := d.imeComposing()
	if ev.Name == "Esc" && composing {
		d.imeCommit("")
		return true
	}
	key := ""
	switch ev.Name {
	case "Backspace":
		if composing {
			key = "Backspace"
		}
	case "Space":
		if composing {
			key = "Space"
		}
	case "Enter":
		if composing {
			key = "Enter"
		}
	default:
		r := ev.Rune
		if r == 0 && len([]rune(ev.Name)) == 1 {
			r = []rune(ev.Name)[0]
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			key = string(r)
		} else if composing && r >= 32 {
			d.imeCommit(d.ime.engine.Composition())
			return false
		}
	}
	if key == "" {
		return false
	}
	if d.ime.engine == nil {
		d.ime.engine = ime.New()
	}
	result := d.ime.engine.Handle(key)
	if !result.Handled {
		return false
	}
	d.ime.candidates = result.Candidates
	if result.Committed != "" {
		d.imeCommit(result.Committed)
	}
	return true
}

func (d *Desktop) imeTrayRect() ui.Rect {
	lan := d.lanTrayRect()
	return ui.Rect{lan.X - 34, lan.Y, 30, lan.H}
}

func (d *Desktop) drawIMETray(c *ui.Canvas) {
	r := d.imeTrayRect()
	c.FillRect(r, ui.ColFace)
	c.Sunken(r)
	label := "EN"
	if d.ime.enabled {
		label = "拼"
	}
	f := ui.DefaultFont()
	f.Text(c, r.X+(r.W-f.Width(label))/2, r.Y+4, label, ui.ColText)
}

func (d *Desktop) imeMouse(ev MouseEvent) bool {
	if ev.Kind != MouseDown || ev.Button != 1 || !d.imeComposing() {
		return false
	}
	for i, r := range d.ime.rects {
		if r.Contains(ev.X, ev.Y) && i < len(d.ime.candidates) {
			d.imeCommit(d.ime.candidates[i].Text)
			return true
		}
	}
	return false
}

func (d *Desktop) drawIME(c *ui.Canvas) {
	d.ime.rects = nil
	d.ime.window = ui.Rect{}
	if !d.ime.enabled || !d.imeComposing() || d.imeCurrent() != d.ime.owner {
		return
	}
	f := ui.DefaultFont()
	comp := strings.Join(d.ime.engine.Syllables(), " ")
	width := f.Width(comp) + 18
	for _, candidate := range d.ime.candidates {
		width += f.Width(candidate.Text) + 28
	}
	width = maxInt(120, minInt(width, d.W-8))
	height := 50
	x := maxInt(4, minInt(d.ime.anchor.X, d.W-width-4))
	y := d.ime.anchor.Y - height - 4
	if y < 4 {
		y = minInt(d.ime.anchor.Bottom()+4, d.taskbarTop()-height-4)
	}
	r := ui.Rect{x, y, width, height}
	d.ime.window = r
	c.FillRect(ui.Rect{r.X + 2, r.Y + 2, r.W, r.H}, ui.ColShadow)
	c.FillRect(r, ui.ColFace)
	c.Frame(r, ui.ColDark)
	f.TextClip(c, r.X+6, r.Y+4, r.W-12, comp, ui.ColText)
	xx := r.X + 5
	for i, candidate := range d.ime.candidates {
		label := string(rune('1' + i))
		if i == 9 {
			label = "0"
		}
		label += " " + candidate.Text
		w := f.Width(label) + 10
		if xx+w > r.Right()-4 {
			break
		}
		cr := ui.Rect{xx, r.Y + 25, w, 20}
		c.FillRect(cr, ui.ColField)
		c.Frame(cr, ui.ColShadow)
		f.Text(c, cr.X+4, cr.Y+3, label, ui.ColText)
		d.ime.rects = append(d.ime.rects, cr)
		xx += w + 3
	}
}
