package desktop

import (
	"strings"

	"win95linux/internal/ui"
)

type InputBoxApp struct {
	Base
	d      *Desktop
	title  string
	prompt string
	value  []rune
	caret  int
	cb     func(string, bool)
	win    *Window
	press  string
	err    string

	fieldR, okR, cancelR ui.Rect
}

func (d *Desktop) InputBox(title, prompt, initial string, cb func(string, bool)) *Window {
	a := &InputBoxApp{d: d, title: title, prompt: prompt, cb: cb}
	a.value = []rune(initial)
	a.caret = len(a.value)
	w := d.OpenModal(a)
	a.win = w
	return w
}

func (a *InputBoxApp) Title() string           { return a.title }
func (a *InputBoxApp) Icon() ui.IconKind       { return ui.IcoInfo }
func (a *InputBoxApp) DefaultSize() (int, int) { return 440, 176 }
func (a *InputBoxApp) MinSize() (int, int)     { return 440, 176 }
func (a *InputBoxApp) Resizable() bool         { return false }

func (a *InputBoxApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.TextClip(c, r.X+16, r.Y+14, r.W-32, a.prompt, ui.ColText)

	a.fieldR = ui.Rect{r.X + 16, r.Y + 40, r.W - 32, 27}
	c.FillRect(a.fieldR, ui.ColWhite)
	c.Sunken(a.fieldR)
	f.TextClip(c, a.fieldR.X+5, a.fieldR.Y+5, a.fieldR.W-10, string(a.value), ui.ColText)
	before := string(a.value[:a.caret])
	cx := a.fieldR.X + 5 + f.Width(before)
	if cx < a.fieldR.Right()-4 {
		c.VLine(cx, a.fieldR.Y+4, 18, ui.ColText)
	}

	a.okR = ui.Rect{r.Right() - 196, r.Bottom() - 42, 84, 26}
	a.cancelR = ui.Rect{r.Right() - 104, r.Bottom() - 42, 84, 26}
	ui.Button(c, a.okR, "确定", a.press == "ok", true)
	ui.Button(c, a.cancelR, "取消", a.press == "cancel", true)
	if a.err != "" {
		f.TextClip(c, r.X+16, r.Y+74, r.W-32, a.err, ui.ColRed)
	}
}

func (a *InputBoxApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.okR.Contains(ev.X, ev.Y):
			a.press = "ok"
		case a.cancelR.Contains(ev.X, ev.Y):
			a.press = "cancel"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "ok":
			if a.okR.Contains(ev.X, ev.Y) {
				a.accept()
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) {
				a.finish(false)
			}
		}
	}
}

func (a *InputBoxApp) accept() {
	name := strings.TrimSpace(string(a.value))
	switch {
	case name == "":
		a.err = "名称不能为空。"
	case name == "." || name == "..":
		a.err = "这个名称无效。"
	case strings.ContainsAny(name, "/\\\x00"):
		a.err = "名称中不能包含斜杠。"
	case len([]rune(name)) > 200:
		a.err = "名称太长了。"
	default:
		a.finish(true)
		return
	}
	a.d.Invalidate()
}

func (a *InputBoxApp) finish(ok bool) {
	name := strings.TrimSpace(string(a.value))
	win, cb := a.win, a.cb
	if win != nil {
		a.d.CloseWindowForced(win)
	}
	if cb != nil {
		cb(name, ok)
	}
}

func (a *InputBoxApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		a.finish(false)
		return true
	case "Enter":
		a.accept()
		return true
	case "Backspace":
		if a.caret > 0 {
			a.value = append(a.value[:a.caret-1], a.value[a.caret:]...)
			a.caret--
		}
		return true
	case "Delete":
		if a.caret < len(a.value) {
			a.value = append(a.value[:a.caret], a.value[a.caret+1:]...)
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
		}
		return true
	case "Right":
		if a.caret < len(a.value) {
			a.caret++
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(a.value)
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.value = append(a.value, 0)
		copy(a.value[a.caret+1:], a.value[a.caret:])
		a.value[a.caret] = ev.Rune
		a.caret++
		a.err = ""
		return true
	}
	return false
}

func (a *InputBoxApp) Close() {}
