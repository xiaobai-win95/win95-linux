package desktop

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"win95linux/internal/ui"
)

type CalcApp struct {
	Base
	display string
	acc     float64
	pending string
	fresh   bool
	lastOp  string
	lastArg float64
	err     bool
	buttons []ui.Rect
	labels  []string
	press   int
	win     *Window
}

func NewCalcApp() *CalcApp {
	return &CalcApp{display: "0", fresh: true}
}

func (a *CalcApp) Title() string           { return "计算器" }
func (a *CalcApp) Icon() ui.IconKind       { return ui.IcoCalc }
func (a *CalcApp) DefaultSize() (int, int) { return 292, 340 }
func (a *CalcApp) MinSize() (int, int)     { return 292, 340 }
func (a *CalcApp) Resizable() bool         { return false }

func (a *CalcApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "复制结果", Accel: "Ctrl+C", Enabled: true, Action: func() {}},
			{Separator: true},
			{Label: "清除(C)", Enabled: true, Action: func() { a.clearAll() }},
			{Label: "清除当前(CE)", Enabled: true, Action: func() { a.clearEntry() }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "标准型", Checked: true, Enabled: true, Action: func() {}},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于计算器(A)", Enabled: true, Action: func() {
				d.MessageBox("关于计算器", "Windows 95 Linux 计算器\n版本 1.0\n\n支持键盘输入与基本四则运算。", IconInfo, []string{"确定"}, nil)
			}},
			{Separator: true},
			{Label: "关闭", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
	}
}

func (a *CalcApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	disp := ui.Rect{r.X + 8, r.Y + 8, r.W - 16, 32}
	c.FillRect(disp, ui.ColWhite)
	c.Sunken(disp)
	col := ui.ColText
	if a.err {
		col = ui.ColRed
	}
	text := a.display
	if f.Width(text) > disp.W-10 {
		for f.Width(text) > disp.W-10 && len(text) > 1 {
			text = text[1:]
		}
	}
	f.Text(c, disp.Right()-6-f.Width(text), disp.Y+8, text, col)

	gap := 6
	top := disp.Bottom() + 8
	bw := (r.W - 16 - 3*gap) / 4
	bh := (r.Bottom() - top - 8 - 4*gap) / 5
	if bh > 42 {
		bh = 42
	}
	if bh < 26 {
		bh = 26
	}
	labels := []string{
		"C", "CE", "←", "÷",
		"7", "8", "9", "×",
		"4", "5", "6", "-",
		"1", "2", "3", "+",
		"±", "0", ".", "=",
	}
	a.buttons = a.buttons[:0]
	a.labels = labels
	for i, label := range labels {
		colIdx := i % 4
		row := i / 4
		br := ui.Rect{r.X + 8 + colIdx*(bw+gap), top + row*(bh+gap), bw, bh}
		pressed := a.press == i
		ui.Button(c, br, label, pressed, true)
		if label == "=" {
			c.Frame(ui.Rect{br.X + 2, br.Y + 2, br.W - 4, br.H - 4}, ui.ColShadow)
		}
		a.buttons = append(a.buttons, br)
	}
}

func (a *CalcApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = -1
		for i, b := range a.buttons {
			if b.Contains(ev.X, ev.Y) {
				a.press = i
			}
		}
	case MouseUp:
		p := a.press
		a.press = -1
		if p >= 0 && p < len(a.buttons) && a.buttons[p].Contains(ev.X, ev.Y) {
			a.pressKey(a.labels[p])
		}
	}
}

func (a *CalcApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl {
		return false
	}
	switch ev.Name {
	case "Enter", "=":
		a.pressKey("=")
		return true
	case "Backspace":
		a.pressKey("←")
		return true
	case "Delete":
		a.pressKey("CE")
		return true
	case "Esc":
		a.pressKey("C")
		return true
	}
	if ev.Rune >= '0' && ev.Rune <= '9' {
		a.pressKey(string(ev.Rune))
		return true
	}
	switch ev.Rune {
	case '+', '-', '*', '/', '.', '%':
		a.pressKey(string(ev.Rune))
		return true
	}
	return false
}

func (a *CalcApp) clearAll() {
	a.display = "0"
	a.acc = 0
	a.pending = ""
	a.fresh = true
	a.err = false
	a.lastOp = ""
}

func (a *CalcApp) clearEntry() {
	a.display = "0"
	a.fresh = true
	if a.err {
		a.err = false
		a.acc = 0
		a.pending = ""
	}
}

func (a *CalcApp) value() float64 {
	v, err := strconv.ParseFloat(a.display, 64)
	if err != nil {
		return 0
	}
	return v
}

func formatNumber(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return "除数不能为零"
	}
	if math.Abs(v) >= 1e15 {
		return strconv.FormatFloat(v, 'g', 12, 64)
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 16 {
		s = strconv.FormatFloat(v, 'f', 8, 64)
	}
	return s
}

func (a *CalcApp) pressKey(k string) {
	if a.err {
		a.clearAll()
	}
	switch k {
	case "C":
		a.clearAll()
		return
	case "CE":
		a.clearEntry()
		return
	case "←":
		if a.fresh {
			return
		}
		if len(a.display) <= 1 || (len(a.display) == 2 && a.display[0] == '-') {
			a.display = "0"
			a.fresh = true
			return
		}
		a.display = a.display[:len(a.display)-1]
		return
	case "±":
		if a.display == "0" {
			return
		}
		if strings.HasPrefix(a.display, "-") {
			a.display = a.display[1:]
		} else {
			a.display = "-" + a.display
		}
		return
	case ".":
		if a.fresh {
			a.display = "0."
			a.fresh = false
			return
		}
		if !strings.Contains(a.display, ".") {
			a.display += "."
		}
		return
	case "÷", "/":
		a.setOp("/")
		return
	case "×", "*":
		a.setOp("*")
		return
	case "-":
		a.setOp("-")
		return
	case "+":
		a.setOp("+")
		return
	case "%":
		a.display = formatNumber(a.value() / 100)
		a.fresh = true
		return
	case "=":
		a.equals()
		return
	}
	if k >= "0" && k <= "9" {
		if a.fresh || a.display == "0" {
			a.display = k
			a.fresh = false
		} else if len(a.display) < 18 {
			a.display += k
		}
	}
}

func (a *CalcApp) setOp(op string) {
	cur := a.value()
	if a.pending != "" && !a.fresh {
		cur = a.apply(a.acc, cur, a.pending)
		a.display = formatNumber(cur)
		if a.display == "除数不能为零" {
			a.err = true
			a.pending = ""
			a.fresh = true
			return
		}
	}
	a.acc = cur
	a.pending = op
	a.fresh = true
}

func (a *CalcApp) apply(x, y float64, op string) float64 {
	switch op {
	case "+":
		return x + y
	case "-":
		return x - y
	case "*":
		return x * y
	case "/":
		if y == 0 {
			return math.Inf(1)
		}
		return x / y
	}
	return y
}

func (a *CalcApp) equals() {
	cur := a.value()
	if a.pending != "" {
		a.lastOp = a.pending
		a.lastArg = cur
		res := a.apply(a.acc, cur, a.pending)
		a.acc = res
		a.display = formatNumber(res)
		a.pending = ""
		a.err = a.display == "除数不能为零"
		a.fresh = true
		if a.err {
			a.acc = 0
		}
		return
	}
	if a.lastOp != "" {
		res := a.apply(cur, a.lastArg, a.lastOp)
		a.display = formatNumber(res)
		a.err = a.display == "除数不能为零"
		a.fresh = true
	}
}

func (a *CalcApp) Close() {}

func (a *CalcApp) Status() string {
	return fmt.Sprintf("%s", a.display)
}
