package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const iseSampleScript = `# Windows PowerShell ISE 示例脚本
# 按 F5 运行整个脚本。

Write-Host "你好，Windows 95 Linux！" -ForegroundColor Yellow

$name = "世界"
Write-Output "你好，$name"

Get-Date
Get-ChildItem C:\root | Select-Object -First 6
`

// PShellISEApp is the PowerShell ISE panel: an editor on top, command output
// below, sharing the same cmdlet engine as the console.
type PShellISEApp struct {
	Base
	d    *Desktop
	win  *Window
	sess *psSession

	lines    []string
	caretLn  int
	caretCol int
	path     string
	modified bool
	loadErr  string

	out       []psLine
	outScroll VScroll
	outStick  bool
	edit      VScroll

	busy   bool
	status string
	closed bool
	press  string

	btnRun, btnStop, btnNew, btnOpen, btnSave, btnClear  ui.Rect
	editorR, editTextR, gutterR, outR, outBodyR, statusR ui.Rect
}

func NewPShellISEApp(d *Desktop) *PShellISEApp {
	a := &PShellISEApp{d: d, sess: newPSSession(), outStick: true}
	a.lines = strings.Split(strings.TrimSuffix(iseSampleScript, "\n"), "\n")
	a.out = []psLine{
		{text: "Windows PowerShell ISE 26.2", col: psColPrompt, bold: true},
		{text: "编辑脚本后按 F5 运行。Ctrl+S 保存，Ctrl+O 打开。", col: psColDim},
		{text: ""},
	}
	return a
}

func (a *PShellISEApp) Title() string {
	name := "无标题.ps1"
	if a.path != "" {
		name = filepath.Base(a.path)
	}
	mark := ""
	if a.modified {
		mark = "*"
	}
	if a.busy {
		return name + mark + " - PowerShell ISE（正在运行）"
	}
	return name + mark + " - PowerShell ISE"
}

func (a *PShellISEApp) Icon() ui.IconKind       { return ui.IcoISE }
func (a *PShellISEApp) DefaultSize() (int, int) { return 860, 600 }
func (a *PShellISEApp) MinSize() (int, int)     { return 480, 340 }
func (a *PShellISEApp) Resizable() bool         { return true }

func (a *PShellISEApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "新建(N)", Accel: "Ctrl+N", Enabled: true, Action: func() { a.newScript(d) }},
			{Label: "打开(O)…", Accel: "Ctrl+O", Enabled: true, Action: func() { a.openDialog(d) }},
			{Label: "保存(S)", Accel: "Ctrl+S", Enabled: true, Action: func() { a.saveThen(func(bool) {}) }},
			{Label: "另存为(A)…", Enabled: true, Action: func() { a.saveAs(func(bool) {}) }},
			{Separator: true},
			{Label: "退出(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "运行(R)", Items: []MenuItem{
			{Label: "运行脚本(F5)", Enabled: !a.busy, Action: func() { a.run() }},
			{Label: "停止(Esc)", Enabled: a.busy, Action: func() { a.stop() }},
			{Separator: true},
			{Label: "清空输出(L)", Enabled: true, Action: func() { a.clearOutput() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "命令帮助(H)", Enabled: true, Action: func() { a.insertText("Get-Help") }},
			{Separator: true},
			{Label: "关于 PowerShell ISE(A)", Enabled: true, Action: func() {
				d.MessageBox("关于 Windows PowerShell ISE",
					"Windows PowerShell ISE 26.2\nWindows 95 Linux 中文版\n\n上部编辑脚本，下部显示输出。\nF5 运行，Esc 停止。\n与 PowerShell 控制台共享同一套 cmdlet。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *PShellISEApp) layout(client ui.Rect) {
	a.statusR = ui.Rect{client.X, client.Bottom() - 20, client.W, 20}
	top := client.Y + 34
	avail := client.H - 34 - 20
	if avail < 100 {
		avail = 100
	}
	outH := avail * 38 / 100
	if outH < 90 {
		outH = 90
	}
	edH := avail - outH - 6
	if edH < 80 {
		edH = 80
	}
	a.editorR = ui.Rect{client.X + 4, top, client.W - 8, edH}
	a.outR = ui.Rect{client.X + 4, top + edH + 6, client.W - 8, outH}
	inner := a.editorR.Inset(2)
	a.gutterR = ui.Rect{inner.X, inner.Y, 34, inner.H}
	a.editTextR = ui.Rect{inner.X + 34, inner.Y + 1, inner.W - 34 - 17, inner.H - 2}
	if a.editTextR.W < 32 {
		a.editTextR.W = 32
	}
	a.outBodyR = ui.Rect{a.outR.X + 2, a.outR.Y + 20, a.outR.W - 20, a.outR.H - 22}
}

func (a *PShellISEApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, ui.ColFace)
	a.layout(client)
	f := ui.DefaultFont()

	// Toolbar.
	tx := client.X + 4
	ty := client.Y + 4
	type btn struct {
		label string
		r     *ui.Rect
		id    string
	}
	buttons := []btn{
		{"运行", &a.btnRun, "run"},
		{"停止", &a.btnStop, "stop"},
		{"新建", &a.btnNew, "new"},
		{"打开", &a.btnOpen, "open"},
		{"保存", &a.btnSave, "save"},
		{"清空输出", &a.btnClear, "clear"},
	}
	for _, b := range buttons {
		width := 60
		if b.id == "clear" {
			width = 80
		}
		*b.r = ui.Rect{tx, ty, width, 24}
		enabled := true
		switch b.id {
		case "run":
			enabled = !a.busy
		case "stop":
			enabled = a.busy
		case "clear", "new", "open", "save":
			enabled = !a.busy
		}
		ui.Button(c, *b.r, b.label, a.press == b.id, enabled)
		tx += width + 4
	}
	c.HLine(client.X+4, client.Y+31, client.W-8, ui.ColShadow)
	c.HLine(client.X+4, client.Y+32, client.W-8, ui.ColWhite)

	// Editor.
	c.FillRect(a.editorR, ui.ColWhite)
	c.Sunken(a.editorR)
	c.FillRect(a.gutterR, 0xE8E8E8)
	c.VLine(a.gutterR.Right()-1, a.gutterR.Y, a.gutterR.H, 0xC0C0C0)
	rows := a.editTextR.H / 16
	if rows < 1 {
		rows = 1
	}
	a.edit.Total = len(a.lines)
	a.edit.Visible = rows
	maxScroll := len(a.lines) - rows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.edit.Pos > maxScroll {
		a.edit.Pos = maxScroll
	}
	if a.edit.Pos < 0 {
		a.edit.Pos = 0
	}
	active := d.Active() == w
	for i := 0; i < rows; i++ {
		ln := a.edit.Pos + i
		if ln >= len(a.lines) {
			break
		}
		y := a.editTextR.Y + i*16
		if ln == a.caretLn && active {
			c.FillRect(ui.Rect{a.editTextR.X - 34, y, a.editTextR.W + 34 + 16, 16}, 0xE8E4C8)
		}
		num := fmt.Sprintf("%4d", ln+1)
		f.TextRight(c, a.gutterR.Right()-5, y, num, ui.ColShadow)
		f.TextClip(c, a.editTextR.X, y, a.editTextR.W, a.lines[ln], ui.ColText)
	}
	if active {
		row := a.caretLn - a.edit.Pos
		if row >= 0 && row < rows {
			runes := []rune(a.lines[a.caretLn])
			x := 0
			for i := 0; i < a.caretCol && i < len(runes); i++ {
				x += f.RuneWidth(runes[i])
			}
			c.VLine(a.editTextR.X+x, a.editTextR.Y+row*16, 16, ui.ColText)
		}
	}
	a.edit.Draw(c, ui.Rect{a.editorR.Right() - 17, a.editorR.Y + 2, 16, a.editorR.H - 4})

	// Output pane.
	c.FillRect(a.outR, ui.ColFace)
	c.Sunken(a.outR)
	c.FillRect(ui.Rect{a.outR.X + 2, a.outR.Y + 2, a.outR.W - 4, 18}, ui.ColFace)
	f.Text(c, a.outR.X+6, a.outR.Y+3, "输出", ui.ColText)
	if a.status != "" {
		f.TextRight(c, a.outR.Right()-22, a.outR.Y+3, a.status, ui.ColShadow)
	}
	c.FillRect(a.outBodyR, psColBg)
	outRows := a.buildOutRows(a.outBodyR.W)
	a.outScroll.Total = len(outRows)
	a.outScroll.Visible = a.outBodyR.H / 16
	if a.outScroll.Visible < 1 {
		a.outScroll.Visible = 1
	}
	if a.outStick {
		a.outScroll.Pos = a.outScroll.Max()
	}
	a.outScroll.Clamp()
	for i := 0; i < a.outScroll.Visible; i++ {
		idx := a.outScroll.Pos + i
		if idx >= len(outRows) {
			break
		}
		y := a.outBodyR.Y + i*16
		x := a.outBodyR.X
		for _, seg := range outRows[idx].segs {
			if seg.bold {
				f.TextBold(c, x, y, seg.text, seg.col)
			} else {
				f.Text(c, x, y, seg.text, seg.col)
			}
			x += f.Width(seg.text)
		}
	}
	a.outScroll.Draw(c, ui.Rect{a.outR.Right() - 17, a.outR.Y + 20, 16, a.outR.H - 22})

	// Status bar.
	st := a.statusR
	c.FillRect(st, ui.ColFace)
	c.HLine(st.X, st.Y, st.W, ui.ColShadow)
	c.HLine(st.X, st.Y+1, st.W, ui.ColWhite)
	pos := fmt.Sprintf("第 %d 行，第 %d 列", a.caretLn+1, a.caretCol+1)
	f.Text(c, st.X+6, st.Y+3, pos, ui.ColText)
	right := "未保存"
	if a.path != "" {
		right = a.path
	}
	if a.loadErr != "" {
		f.TextClip(c, st.X+180, st.Y+3, st.W-190, a.loadErr, ui.ColRed)
	} else {
		f.TextClip(c, st.X+180, st.Y+3, st.W-190, right, ui.ColShadow)
	}
}

func (a *PShellISEApp) buildOutRows(width int) []psRow {
	if width < 8 {
		width = 8
	}
	var rows []psRow
	out := a.out
	if len(out) == 0 {
		out = []psLine{{text: "按 F5 运行脚本，输出将显示在这里。", col: psColDim}}
	}
	for _, ln := range out {
		text := ln.text
		col := ln.col
		if col == 0 {
			col = psColText
		}
		if text == "" {
			rows = append(rows, psRow{})
			continue
		}
		runes := []rune(text)
		for _, sp := range psWrapSpans(text, width) {
			rows = append(rows, psRow{segs: []psSeg{{text: string(runes[sp[0]:sp[1]]), col: col, bold: ln.bold}}})
		}
	}
	return rows
}

func (a *PShellISEApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	if ev.Kind == MouseWheel {
		if a.outR.Contains(ev.X, ev.Y) {
			a.outScroll.ScrollBy(-ev.Wheel * 3)
			a.outStick = a.outScroll.Pos >= a.outScroll.Max()
		} else if a.editorR.Contains(ev.X, ev.Y) {
			a.edit.ScrollBy(-ev.Wheel * 3)
		}
		d.Invalidate()
		return
	}
	if changed, consumed := a.outScroll.Mouse(ev, ui.Rect{a.outR.Right() - 17, a.outR.Y + 20, 16, a.outR.H - 22}); consumed {
		if changed {
			a.outStick = a.outScroll.Pos >= a.outScroll.Max()
			d.Invalidate()
		}
		return
	}
	if changed, consumed := a.edit.Mouse(ev, ui.Rect{a.editorR.Right() - 17, a.editorR.Y + 2, 16, a.editorR.H - 4}); consumed {
		if changed {
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnRun.Contains(ev.X, ev.Y):
			a.press = "run"
		case a.btnStop.Contains(ev.X, ev.Y):
			a.press = "stop"
		case a.btnNew.Contains(ev.X, ev.Y):
			a.press = "new"
		case a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnSave.Contains(ev.X, ev.Y):
			a.press = "save"
		case a.btnClear.Contains(ev.X, ev.Y):
			a.press = "clear"
		case a.editorR.Contains(ev.X, ev.Y):
			a.placeCaret(ev.X, ev.Y)
			d.Invalidate()
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "run":
			if a.btnRun.Contains(ev.X, ev.Y) {
				a.run()
			}
		case "stop":
			if a.btnStop.Contains(ev.X, ev.Y) {
				a.stop()
			}
		case "new":
			if a.btnNew.Contains(ev.X, ev.Y) {
				a.newScript(d)
			}
		case "open":
			if a.btnOpen.Contains(ev.X, ev.Y) {
				a.openDialog(d)
			}
		case "save":
			if a.btnSave.Contains(ev.X, ev.Y) {
				a.saveThen(func(bool) {})
			}
		case "clear":
			if a.btnClear.Contains(ev.X, ev.Y) {
				a.clearOutput()
			}
		}
		d.Invalidate()
	}
}

func (a *PShellISEApp) placeCaret(x, y int) {
	ln := a.edit.Pos + (y-a.editTextR.Y)/16
	if ln < 0 {
		ln = 0
	}
	if ln >= len(a.lines) {
		ln = len(a.lines) - 1
	}
	a.caretLn = ln
	f := ui.DefaultFont()
	runes := []rune(a.lines[ln])
	col := 0
	acc := 0
	for col < len(runes) {
		rw := f.RuneWidth(runes[col])
		if acc+rw/2 > x-a.editTextR.X {
			break
		}
		acc += rw
		col++
	}
	a.caretCol = col
	a.ensureCaret()
}

func (a *PShellISEApp) ensureCaret() {
	if len(a.lines) == 0 {
		a.lines = []string{""}
	}
	if a.caretLn < 0 {
		a.caretLn = 0
	}
	if a.caretLn >= len(a.lines) {
		a.caretLn = len(a.lines) - 1
	}
	n := len([]rune(a.lines[a.caretLn]))
	if a.caretCol < 0 {
		a.caretCol = 0
	}
	if a.caretCol > n {
		a.caretCol = n
	}
	rows := a.editTextR.H / 16
	if rows < 1 {
		rows = 1
	}
	if a.caretLn < a.edit.Pos {
		a.edit.Pos = a.caretLn
	}
	if a.caretLn >= a.edit.Pos+rows {
		a.edit.Pos = a.caretLn - rows + 1
	}
	if a.edit.Pos < 0 {
		a.edit.Pos = 0
	}
}

func (a *PShellISEApp) insertText(s string) {
	runes := []rune(a.lines[a.caretLn])
	ins := []rune(s)
	out := make([]rune, 0, len(runes)+len(ins))
	out = append(out, runes[:a.caretCol]...)
	out = append(out, ins...)
	out = append(out, runes[a.caretCol:]...)
	a.lines[a.caretLn] = string(out)
	a.caretCol += len(ins)
	a.modified = true
	a.ensureCaret()
	a.d.Invalidate()
}

func (a *PShellISEApp) insertNewline() {
	runes := []rune(a.lines[a.caretLn])
	left := string(runes[:a.caretCol])
	right := string(runes[a.caretCol:])
	a.lines[a.caretLn] = left
	a.lines = append(a.lines, "")
	copy(a.lines[a.caretLn+2:], a.lines[a.caretLn+1:])
	a.lines[a.caretLn+1] = right
	a.caretLn++
	a.caretCol = 0
	a.modified = true
	a.ensureCaret()
}

func (a *PShellISEApp) backspace() {
	if a.caretCol > 0 {
		runes := []rune(a.lines[a.caretLn])
		a.lines[a.caretLn] = string(append(runes[:a.caretCol-1], runes[a.caretCol:]...))
		a.caretCol--
		a.modified = true
		return
	}
	if a.caretLn > 0 {
		prev := []rune(a.lines[a.caretLn-1])
		a.caretCol = len(prev)
		a.lines[a.caretLn-1] = string(prev) + a.lines[a.caretLn]
		a.lines = append(a.lines[:a.caretLn], a.lines[a.caretLn+1:]...)
		a.caretLn--
		a.modified = true
	}
}

func (a *PShellISEApp) deleteChar() {
	runes := []rune(a.lines[a.caretLn])
	if a.caretCol < len(runes) {
		a.lines[a.caretLn] = string(append(runes[:a.caretCol], runes[a.caretCol+1:]...))
		a.modified = true
		return
	}
	if a.caretLn < len(a.lines)-1 {
		a.lines[a.caretLn] += a.lines[a.caretLn+1]
		a.lines = append(a.lines[:a.caretLn+1], a.lines[a.caretLn+2:]...)
		a.modified = true
	}
}

func (a *PShellISEApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl && !ev.Shift {
		switch strings.ToUpper(ev.Name) {
		case "S":
			a.saveThen(func(bool) {})
			return true
		case "N":
			a.newScript(d)
			return true
		case "O":
			a.openDialog(d)
			return true
		case "L":
			a.clearOutput()
			return true
		case "V":
			a.paste()
			return true
		case "C":
			if a.caretLn < len(a.lines) {
				clipboardWrite(a.lines[a.caretLn])
				a.status = "已复制当前行"
				a.d.Invalidate()
			}
			return true
		case "HOME":
			a.caretLn, a.caretCol = 0, 0
			a.ensureCaret()
			a.d.Invalidate()
			return true
		case "END":
			a.caretLn = len(a.lines) - 1
			a.caretCol = len([]rune(a.lines[a.caretLn]))
			a.ensureCaret()
			a.d.Invalidate()
			return true
		case "ENTER":
			a.run()
			return true
		}
	}
	switch ev.Name {
	case "F5":
		a.run()
		return true
	case "Esc":
		if a.busy {
			a.stop()
			return true
		}
		return false
	case "Enter":
		a.insertNewline()
		a.d.Invalidate()
		return true
	case "Tab":
		a.insertText("    ")
		return true
	case "Backspace":
		a.backspace()
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "Delete":
		a.deleteChar()
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "Left":
		if a.caretCol > 0 {
			a.caretCol--
		} else if a.caretLn > 0 {
			a.caretLn--
			a.caretCol = len([]rune(a.lines[a.caretLn]))
		}
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "Right":
		if a.caretCol < len([]rune(a.lines[a.caretLn])) {
			a.caretCol++
		} else if a.caretLn < len(a.lines)-1 {
			a.caretLn++
			a.caretCol = 0
		}
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "Up":
		if ev.Ctrl {
			a.edit.ScrollBy(-1)
		} else if a.caretLn > 0 {
			a.caretLn--
			a.ensureCaret()
		}
		a.d.Invalidate()
		return true
	case "Down":
		if ev.Ctrl {
			a.edit.ScrollBy(1)
		} else if a.caretLn < len(a.lines)-1 {
			a.caretLn++
			a.ensureCaret()
		}
		a.d.Invalidate()
		return true
	case "Home":
		a.caretCol = 0
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "End":
		a.caretCol = len([]rune(a.lines[a.caretLn]))
		a.ensureCaret()
		a.d.Invalidate()
		return true
	case "PgUp":
		a.edit.ScrollBy(-a.edit.Visible)
		a.d.Invalidate()
		return true
	case "PgDn":
		a.edit.ScrollBy(a.edit.Visible)
		a.d.Invalidate()
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.insertText(string(ev.Rune))
		return true
	}
	return false
}

func (a *PShellISEApp) paste() {
	s := clipboardRead()
	if s == "" {
		return
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		if i > 0 {
			a.insertNewline()
		}
		if p != "" {
			a.insertText(p)
		}
	}
	a.d.Invalidate()
}

func (a *PShellISEApp) run() {
	if a.busy {
		return
	}
	script := strings.Join(a.lines, "\n")
	if strings.TrimSpace(script) == "" {
		a.status = "脚本为空"
		a.d.Invalidate()
		return
	}
	a.busy = true
	a.status = "正在运行…"
	a.out = append(a.out, psLine{text: "— 运行 " + time.Now().Format("15:04:05") + " —", col: psColDim})
	a.outStick = true
	a.d.Invalidate()
	go func() {
		var out []psLine
		clear, exit := false, false
		for _, line := range strings.Split(script, "\n") {
			if strings.TrimSpace(psStripComment(line)) == "" {
				continue
			}
			lines, c, e := a.sess.Execute(line)
			out = append(out, lines...)
			if c {
				out = nil
				clear = true
			}
			if e {
				exit = true
				break
			}
		}
		a.d.Post(func() {
			if a.closed {
				return
			}
			if clear {
				a.out = nil
			}
			a.out = append(a.out, out...)
			a.busy = false
			a.status = "就绪"
			a.outStick = true
			a.d.Invalidate()
			if exit && a.win != nil {
				a.d.CloseWindowForced(a.win)
			}
		})
	}()
}

func (a *PShellISEApp) stop() {
	if !a.busy {
		return
	}
	a.sess.cancelCurrent()
	a.status = "正在停止…"
	a.d.Invalidate()
}

func (a *PShellISEApp) clearOutput() {
	a.out = nil
	a.outScroll.Pos = 0
	a.outStick = true
	a.d.Invalidate()
}

func (a *PShellISEApp) SetScript(path, text string) {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	a.lines = strings.Split(text, "\n")
	if len(a.lines) == 0 {
		a.lines = []string{""}
	}
	a.path = path
	a.modified = false
	a.caretLn, a.caretCol = 0, 0
	a.edit.Pos = 0
}

func (a *PShellISEApp) openDialog(d *Desktop) {
	if a.busy {
		return
	}
	dir := "/root/文档"
	if a.path != "" {
		dir = filepath.Dir(a.path)
	}
	d.FileDialog(FileOpen, dir, "", func(path string, ok bool) {
		if !ok {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			a.loadErr = "无法打开文件：" + err.Error()
			d.Invalidate()
			return
		}
		a.SetScript(path, string(b))
		a.loadErr = ""
		d.Invalidate()
	})
}

func (a *PShellISEApp) saveThen(done func(bool)) {
	if done == nil {
		done = func(bool) {}
	}
	if a.path == "" {
		a.saveAs(done)
		return
	}
	text := strings.Join(a.lines, "\n")
	if text != "" {
		text += "\n"
	}
	if err := os.WriteFile(a.path, []byte(text), 0o644); err != nil {
		a.d.MessageBox("PowerShell ISE", "保存脚本失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
		done(false)
		return
	}
	a.modified = false
	a.loadErr = ""
	a.d.Invalidate()
	done(true)
}

func (a *PShellISEApp) saveAs(done func(bool)) {
	dir := "/root/文档"
	name := "脚本.ps1"
	if a.path != "" {
		dir = filepath.Dir(a.path)
		name = filepath.Base(a.path)
	}
	a.d.FileDialog(FileSave, dir, name, func(path string, ok bool) {
		if !ok {
			if done != nil {
				done(false)
			}
			return
		}
		if filepath.Ext(path) == "" {
			path += ".ps1"
		}
		a.path = path
		a.saveThen(done)
	})
}

func (a *PShellISEApp) newScript(d *Desktop) {
	if a.busy {
		return
	}
	reset := func() {
		a.lines = []string{""}
		a.path = ""
		a.caretLn, a.caretCol = 0, 0
		a.modified = false
		a.edit.Pos = 0
		a.loadErr = ""
		d.Invalidate()
	}
	if !a.modified {
		reset()
		return
	}
	d.MessageBox("PowerShell ISE", "脚本已经修改，是否保存？", IconQuestion,
		[]string{"保存", "不保存", "取消"}, func(i int) {
			switch i {
			case 0:
				a.saveThen(func(ok bool) {
					if ok {
						reset()
					}
				})
			case 1:
				reset()
			}
		})
}

func (a *PShellISEApp) RequestClose(d *Desktop, w *Window) bool {
	if !a.modified {
		return true
	}
	d.MessageBox("PowerShell ISE", "脚本“"+strings.TrimSuffix(a.Title(), " - PowerShell ISE")+"”已经修改。\n\n是否保存更改？", IconQuestion,
		[]string{"保存", "不保存", "取消"}, func(i int) {
			switch i {
			case 0:
				a.saveThen(func(ok bool) {
					if ok {
						d.CloseWindowForced(w)
					}
				})
			case 1:
				d.CloseWindowForced(w)
			}
		})
	return false
}

func (a *PShellISEApp) Close() {
	a.closed = true
	a.sess.cancelCurrent()
}

func (a *PShellISEApp) String() string { return fmt.Sprintf("ISE(%s)", a.path) }
