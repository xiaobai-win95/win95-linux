package desktop

import (
	"strings"

	"win95linux/internal/ui"
)

type DialogIcon int

const (
	IconInfo DialogIcon = iota
	IconQuestion
	IconWarning
	IconError
)

type dialogApp struct {
	Base
	d       *Desktop
	title   string
	lines   []string
	icon    DialogIcon
	buttons []string
	cb      func(int)
	win     *Window
	rects   []ui.Rect
	press   int
	w, h    int
}

func wrapText(text string, maxW int) []string {
	f := ui.DefaultFont()
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		line := ""
		width := 0
		for _, r := range para {
			rw := f.RuneWidth(r)
			if width+rw > maxW && line != "" {
				out = append(out, line)
				line = ""
				width = 0
			}
			line += string(r)
			width += rw
		}
		out = append(out, line)
	}
	return out
}

func (a *dialogApp) Title() string     { return a.title }
func (a *dialogApp) Icon() ui.IconKind { return ui.IcoInfo }

func (a *dialogApp) DefaultSize() (int, int) { return a.w, a.h }
func (a *dialogApp) MinSize() (int, int)     { return a.w, a.h }
func (a *dialogApp) Resizable() bool         { return false }

func (d *Desktop) MessageBox(title, text string, icon DialogIcon, buttons []string, cb func(int)) *Window {
	if len(buttons) == 0 {
		buttons = []string{"确定"}
	}
	const w = 470
	lines := wrapText(text, w-40-64)
	h := 132 + len(lines)*20 + 9
	if h < 185 {
		h = 185
	}
	a := &dialogApp{d: d, title: title, lines: lines, icon: icon, buttons: buttons, cb: cb, w: w, h: h}
	if icon == IconError {
		beep()
	}
	a.win = d.OpenModal(a)
	return a.win
}

func (a *dialogApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	a.drawIcon(c, r.X+22, r.Y+22)
	f := ui.DefaultFont()
	y := r.Y + 26
	for _, line := range a.lines {
		f.Text(c, r.X+70, y, line, ui.ColText)
		y += 20
	}
	a.rects = a.rects[:0]
	btnW, btnH, gap := 84, 26, 9
	total := len(a.buttons)*btnW + (len(a.buttons)-1)*gap
	x := r.X + (r.W-total)/2
	by := r.Bottom() - 40
	for i, label := range a.buttons {
		br := ui.Rect{x + i*(btnW+gap), by, btnW, btnH}
		ui.Button(c, br, label, a.press == i, true)
		a.rects = append(a.rects, br)
	}
}

func (a *dialogApp) drawIcon(c *ui.Canvas, x, y int) {
	switch a.icon {
	case IconQuestion:
		c.FillRect(ui.Rect{x, y, 32, 32}, ui.ColWhite)
		c.Frame(ui.Rect{x, y, 32, 32}, ui.ColDark)
		ui.QuestionMark(c, x+7, y+4, ui.ColNavy)
	case IconWarning:
		for i := 0; i < 16; i++ {
			for xx := x + 16 - i; xx <= x+16+i; xx++ {
				c.Set(xx, y+2+i, ui.ColYellow)
			}
		}
		c.Frame(ui.Rect{x + 2, y + 2, 28, 28}, ui.ColDark)
		for i := 0; i < 11; i++ {
			c.Set(x+16, y+9+i, ui.ColDark)
		}
		c.FillRect(ui.Rect{x + 15, y + 23, 3, 3}, ui.ColDark)
	case IconError:
		c.FillRect(ui.Rect{x + 4, y + 4, 25, 25}, ui.ColRed)
		c.Frame(ui.Rect{x + 4, y + 4, 25, 25}, ui.ColDark)
		for i := 0; i < 6; i++ {
			c.Set(x+9+i, y+9+i, ui.ColWhite)
			c.Set(x+24-i, y+9+i, ui.ColWhite)
			c.Set(x+9+i, y+21-i, ui.ColWhite)
			c.Set(x+24-i, y+21-i, ui.ColWhite)
		}
	default:
		c.FillRect(ui.Rect{x + 2, y + 2, 28, 28}, ui.ColNavy)
		c.Raised(ui.Rect{x + 2, y + 2, 28, 28})
		ui.DefaultFont().Scaled(c, x+12, y+3, "i", ui.ColWhite, 2)
	}
}

func (a *dialogApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = -1
		for i, r := range a.rects {
			if r.Contains(ev.X, ev.Y) {
				a.press = i
			}
		}
	case MouseUp:
		pressed := a.press
		a.press = -1
		if pressed >= 0 && pressed < len(a.rects) && a.rects[pressed].Contains(ev.X, ev.Y) {
			a.finish(pressed)
		}
	}
}

func (a *dialogApp) finish(idx int) {
	cb := a.cb
	win := a.win
	if win != nil {
		a.d.CloseWindowForced(win)
	}
	if cb != nil {
		cb(idx)
	}
}

func (a *dialogApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		a.finish(-1)
		return true
	case "Enter":
		a.finish(0)
		return true
	}
	return false
}
