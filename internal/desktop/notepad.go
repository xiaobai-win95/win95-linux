package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

type visLine struct {
	line, start, end int
}

type NotepadApp struct {
	Base
	d        *Desktop
	win      *Window
	path     string
	lines    []string
	caretLn  int
	caretCol int
	modified bool
	loadErr  string
	scroll   VScroll
	vis      []visLine
	contentW int

	selAnchorLn  int
	selAnchorCol int
	selActive    bool
	selecting    bool

	btnNew, btnOpen, btnSave, btnSaveAs ui.Rect
	editorR, textR, statusR             ui.Rect
	press                               string
}

func NewNotepadApp(d *Desktop, path string) *NotepadApp {
	a := &NotepadApp{d: d, lines: []string{""}}
	if path != "" {
		a.openPath(path)
	}
	return a
}

func (a *NotepadApp) openPath(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		a.loadErr = "无法打开 " + filepath.Base(path) + ": " + err.Error()
		return
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	a.lines = strings.Split(text, "\n")
	if len(a.lines) == 0 {
		a.lines = []string{""}
	}
	a.path = path
	a.modified = false
	a.caretLn, a.caretCol = 0, 0
	a.scroll.Pos = 0
	a.selActive = false
	a.selecting = false
}

func (a *NotepadApp) SetContent(path, text string) {
	a.lines = strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	a.path = path
	a.modified = false
	a.caretLn, a.caretCol = 0, 0
	a.selActive = false
	a.selecting = false
}

func (a *NotepadApp) Title() string {
	name := "无标题"
	if a.path != "" {
		name = filepath.Base(a.path)
	}
	mark := ""
	if a.modified {
		mark = "*"
	}
	return name + mark + " - 记事本"
}

func (a *NotepadApp) Icon() ui.IconKind       { return ui.IcoNotepad }
func (a *NotepadApp) DefaultSize() (int, int) { return 760, 540 }
func (a *NotepadApp) MinSize() (int, int)     { return 380, 260 }
func (a *NotepadApp) Resizable() bool         { return true }

func (a *NotepadApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "新建(N)", Accel: "Ctrl+N", Enabled: true, Action: func() { a.newDoc(d, w) }},
			{Label: "打开(O)…", Accel: "Ctrl+O", Enabled: true, Action: func() { a.openDialog(d) }},
			{Label: "保存(S)", Accel: "Ctrl+S", Enabled: true, Action: func() { a.saveThen(func(bool) {}) }},
			{Label: "另存为(A)…", Enabled: true, Action: func() { a.saveAs(func(bool) {}) }},
			{Separator: true},
			{Label: "退出(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "剪切(T)", Accel: "Ctrl+X", Enabled: a.selActive, Action: func() { a.cutSelection() }},
			{Label: "复制(C)", Accel: "Ctrl+C", Enabled: a.selActive, Action: func() { a.copySelection() }},
			{Label: "粘贴(P)", Accel: "Ctrl+V", Enabled: clipboardRead() != "", Action: func() { a.pasteFromClipboard() }},
			{Label: "全选(A)", Accel: "Ctrl+A", Enabled: true, Action: func() {
				a.selAnchorLn, a.selAnchorCol = 0, 0
				a.caretLn = len(a.lines) - 1
				a.caretCol = len([]rune(a.lines[a.caretLn]))
				a.selActive = a.caretLn != 0 || a.caretCol != 0
				a.ensureCaret()
				a.clearSelIfEmpty()
				d.Invalidate()
			}},
			{Separator: true},
			{Label: "时间/日期(D)", Accel: "F5", Enabled: true, Action: func() {
				a.selActive = false
				a.insertText(time.Now().Format("2006-01-02 15:04"))
			}},
		}},
		{Label: "格式(O)", Items: []MenuItem{
			{Label: "自动换行(W)", Checked: true, Enabled: true},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于记事本(A)", Enabled: true, Action: func() {
				d.MessageBox("关于记事本", "Windows 95 Linux 记事本\n版本 1.0\n\n支持 UTF-8 中文文本编辑与文件保存。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *NotepadApp) layout(client ui.Rect) {
	toolbarH := 32
	statusH := 22
	a.editorR = ui.Rect{client.X + 2, client.Y + toolbarH, client.W - 4, client.H - toolbarH - statusH - 2}
	a.statusR = ui.Rect{client.X, client.Bottom() - statusH, client.W, statusH}
	inner := a.editorR.Inset(2)
	scrollW := 16
	a.textR = ui.Rect{inner.X + 3, inner.Y + 3, inner.W - scrollW - 6, inner.H - 6}
	if a.textR.W < 32 {
		a.textR.W = 32
	}
	if a.textR.H < 16 {
		a.textR.H = 16
	}
	a.contentW = a.textR.W
}

func (a *NotepadApp) buildVis() {
	f := ui.DefaultFont()
	a.vis = a.vis[:0]
	for li, line := range a.lines {
		runes := []rune(line)
		start := 0
		w := 0
		for i, r := range runes {
			rw := f.RuneWidth(r)
			if i > start && w+rw > a.contentW {
				a.vis = append(a.vis, visLine{li, start, i})
				start = i
				w = 0
			}
			w += rw
		}
		a.vis = append(a.vis, visLine{li, start, len(runes)})
	}
}

func (a *NotepadApp) caretVisual() (int, int) {
	f := ui.DefaultFont()
	best := 0
	bestX := 0
	for i, v := range a.vis {
		if v.line != a.caretLn {
			continue
		}
		if a.caretCol >= v.start && a.caretCol <= v.end {
			x := 0
			runes := []rune(a.lines[v.line])
			for c := v.start; c < a.caretCol && c < len(runes); c++ {
				x += f.RuneWidth(runes[c])
			}
			best = i
			bestX = x
			if a.caretCol < v.end {
				return i, x
			}
		}
	}
	return best, bestX
}

func (a *NotepadApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, ui.ColFace)
	a.layout(client)
	a.buildVis()
	f := ui.DefaultFont()

	// Toolbar.
	tx := client.X + 4
	ty := client.Y + 4
	buttons := []struct {
		label string
		r     *ui.Rect
		id    string
	}{
		{"新建", &a.btnNew, "new"},
		{"打开", &a.btnOpen, "open"},
		{"保存", &a.btnSave, "save"},
		{"另存", &a.btnSaveAs, "saveas"},
	}
	for _, b := range buttons {
		*b.r = ui.Rect{tx, ty, 64, 24}
		ui.Button(c, *b.r, b.label, a.press == b.id, true)
		tx += 68
	}
	c.HLine(client.X+4, client.Y+31, client.W-8, ui.ColShadow)
	c.HLine(client.X+4, client.Y+32, client.W-8, ui.ColWhite)

	// Editor.
	c.FillRect(a.editorR, ui.ColWhite)
	c.Sunken(a.editorR)
	rows := a.textR.H / 16
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.vis)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	for i := 0; i < rows; i++ {
		vi := a.scroll.Pos + i
		if vi >= len(a.vis) {
			break
		}
		v := a.vis[vi]
		runes := []rune(a.lines[v.line])
		if v.start > len(runes) {
			continue
		}
		end := v.end
		if end > len(runes) {
			end = len(runes)
		}
		text := string(runes[v.start:end])
		y := a.textR.Y + i*16
		f.Text(c, a.textR.X, y, text, ui.ColText)
		if s, e, ok := a.selInterval(v, runes); ok {
			x0 := a.textR.X + f.Width(string(runes[v.start:s]))
			x1 := x0 + f.Width(string(runes[s:e]))
			c.FillRect(ui.Rect{x0, y, x1 - x0, 16}, ui.ColNavy)
			f.Text(c, x0, y, string(runes[s:e]), ui.ColWhite)
		}
	}
	if d.Active() == w {
		row, cx := a.caretVisual()
		if row >= a.scroll.Pos && row < a.scroll.Pos+rows {
			y := a.textR.Y + (row-a.scroll.Pos)*16
			c.VLine(a.textR.X+cx, y, 16, ui.ColText)
		}
	}
	sr := ui.Rect{a.editorR.Right() - 18, a.editorR.Y + 2, 16, a.editorR.H - 4}
	a.scroll.Draw(c, sr)

	// Status bar.
	st := a.statusR
	c.FillRect(st, ui.ColFace)
	c.HLine(st.X, st.Y, st.W, ui.ColShadow)
	c.HLine(st.X, st.Y+1, st.W, ui.ColWhite)
	status := fmt.Sprintf("第 %d 行，第 %d 列", a.caretLn+1, a.caretCol+1)
	f.Text(c, st.X+6, st.Y+5, status, ui.ColText)
	right := a.path
	if right == "" {
		right = "未保存"
	}
	if a.loadErr != "" {
		f.TextClip(c, st.X+180, st.Y+5, st.W-190, a.loadErr, ui.ColRed)
	} else {
		f.TextClip(c, st.X+180, st.Y+5, st.W-190, right, ui.ColShadow)
	}
}

func (a *NotepadApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	if ev.Kind == MouseWheel {
		before := a.scroll.Pos
		a.scroll.ScrollBy(-ev.Wheel)
		if before != a.scroll.Pos {
			d.Invalidate()
		}
		return
	}
	if (ui.Rect{a.editorR.Right() - 18, a.editorR.Y + 2, 16, a.editorR.H - 4}).Contains(ev.X, ev.Y) {
		if changed, _ := a.scroll.Mouse(ev, ui.Rect{a.editorR.Right() - 18, a.editorR.Y + 2, 16, a.editorR.H - 4}); changed {
			d.Invalidate()
		}
		if ev.Kind == MouseDown {
			return
		}
	}
	if ev.Kind == MouseMove && a.selecting {
		a.selExtend(ev.X, ev.Y)
		d.Invalidate()
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnNew.Contains(ev.X, ev.Y):
			a.press = "new"
		case a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnSave.Contains(ev.X, ev.Y):
			a.press = "save"
		case a.btnSaveAs.Contains(ev.X, ev.Y):
			a.press = "saveas"
		case a.textR.Contains(ev.X, ev.Y):
			a.placeCaret(ev.X, ev.Y)
			a.selAnchorLn, a.selAnchorCol = a.caretLn, a.caretCol
			a.selActive = false
			a.selecting = true
			d.Invalidate()
		}
	case MouseUp:
		if a.selecting {
			a.selecting = false
			a.clearSelIfEmpty()
			d.Invalidate()
			return
		}
		p := a.press
		a.press = ""
		switch p {
		case "new":
			if a.btnNew.Contains(ev.X, ev.Y) {
				a.newDoc(d, w)
			}
		case "open":
			if a.btnOpen.Contains(ev.X, ev.Y) {
				a.openDialog(d)
			}
		case "save":
			if a.btnSave.Contains(ev.X, ev.Y) {
				a.saveThen(func(bool) {})
			}
		case "saveas":
			if a.btnSaveAs.Contains(ev.X, ev.Y) {
				a.saveAs(func(bool) {})
			}
		}
	}
}

func (a *NotepadApp) placeCaret(x, y int) {
	if len(a.vis) == 0 {
		return
	}
	row := a.scroll.Pos + (y-a.textR.Y)/16
	if row < 0 {
		row = 0
	}
	if row >= len(a.vis) {
		row = len(a.vis) - 1
	}
	v := a.vis[row]
	runes := []rune(a.lines[v.line])
	f := ui.DefaultFont()
	col := v.start
	acc := 0
	for col < v.end {
		rw := f.RuneWidth(runes[col])
		if acc+rw/2 > x-a.textR.X {
			break
		}
		acc += rw
		col++
	}
	a.caretLn = v.line
	a.caretCol = col
	a.ensureCaret()
}

func (a *NotepadApp) selExtend(x, y int) {
	if len(a.vis) == 0 {
		return
	}
	if y < a.textR.Y {
		y = a.textR.Y
	}
	if y >= a.textR.Bottom() {
		y = a.textR.Bottom() - 1
	}
	a.placeCaret(x, y)
	if a.caretLn == a.selAnchorLn && a.caretCol == a.selAnchorCol {
		a.selActive = false
	} else {
		a.selActive = true
	}
}

// selection returns the normalized document range. Columns are clamped to
// their lines because an earlier edit may have shortened the anchor line.
func (a *NotepadApp) selection() (int, int, int, int, bool) {
	if !a.selActive || len(a.lines) == 0 {
		return 0, 0, 0, 0, false
	}
	ln1, col1 := a.selAnchorLn, a.selAnchorCol
	ln2, col2 := a.caretLn, a.caretCol
	if ln1 < 0 {
		ln1 = 0
	}
	if ln2 < 0 {
		ln2 = 0
	}
	if ln1 >= len(a.lines) {
		ln1 = len(a.lines) - 1
	}
	if ln2 >= len(a.lines) {
		ln2 = len(a.lines) - 1
	}
	if n := len([]rune(a.lines[ln1])); col1 > n {
		col1 = n
	}
	if n := len([]rune(a.lines[ln2])); col2 > n {
		col2 = n
	}
	if col1 < 0 {
		col1 = 0
	}
	if col2 < 0 {
		col2 = 0
	}
	if ln1 > ln2 || (ln1 == ln2 && col1 > col2) {
		ln1, col1, ln2, col2 = ln2, col2, ln1, col1
	}
	if ln1 == ln2 && col1 == col2 {
		return 0, 0, 0, 0, false
	}
	return ln1, col1, ln2, col2, true
}

func (a *NotepadApp) clearSelIfEmpty() {
	if _, _, _, _, ok := a.selection(); !ok {
		a.selActive = false
	}
}

// selInterval maps the document selection onto one wrapped visual row.
func (a *NotepadApp) selInterval(v visLine, runes []rune) (int, int, bool) {
	ln1, col1, ln2, col2, ok := a.selection()
	if !ok || v.line < ln1 || v.line > ln2 {
		return 0, 0, false
	}
	s, e := v.start, v.end
	if v.line == ln1 && col1 > s {
		s = col1
	}
	if v.line == ln2 && col2 < e {
		e = col2
	}
	if s > len(runes) {
		s = len(runes)
	}
	if e > len(runes) {
		e = len(runes)
	}
	if s >= e {
		return 0, 0, false
	}
	return s, e, true
}

func (a *NotepadApp) selectedText() string {
	ln1, col1, ln2, col2, ok := a.selection()
	if !ok {
		return ""
	}
	if ln1 == ln2 {
		r := []rune(a.lines[ln1])
		return string(r[col1:col2])
	}
	var b strings.Builder
	r1 := []rune(a.lines[ln1])
	b.WriteString(string(r1[col1:]))
	for ln := ln1 + 1; ln < ln2; ln++ {
		b.WriteByte('\n')
		b.WriteString(a.lines[ln])
	}
	b.WriteByte('\n')
	r2 := []rune(a.lines[ln2])
	b.WriteString(string(r2[:col2]))
	return b.String()
}

func (a *NotepadApp) deleteSelection() bool {
	ln1, col1, ln2, col2, ok := a.selection()
	if !ok {
		return false
	}
	r1 := []rune(a.lines[ln1])
	r2 := []rune(a.lines[ln2])
	merged := string(r1[:col1]) + string(r2[col2:])
	out := make([]string, 0, len(a.lines)-(ln2-ln1))
	out = append(out, a.lines[:ln1]...)
	out = append(out, merged)
	out = append(out, a.lines[ln2+1:]...)
	a.lines = out
	a.caretLn, a.caretCol = ln1, col1
	a.selActive = false
	a.selecting = false
	a.modified = true
	return true
}

func (a *NotepadApp) copySelection() bool {
	s := a.selectedText()
	if s == "" {
		return false
	}
	clipboardWrite(s)
	return true
}

func (a *NotepadApp) cutSelection() bool {
	s := a.selectedText()
	if s == "" {
		return false
	}
	clipboardWrite(s)
	a.deleteSelection()
	a.ensureCaret()
	a.d.Invalidate()
	return true
}

func (a *NotepadApp) insertMultiline(s string) {
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		if i > 0 {
			a.insertNewline()
		}
		if p != "" {
			a.insertText(p)
		}
	}
}

func (a *NotepadApp) pasteFromClipboard() bool {
	s := clipboardRead()
	if s == "" {
		return false
	}
	a.deleteSelection()
	a.insertMultiline(s)
	a.ensureCaret()
	a.d.Invalidate()
	return true
}

func (a *NotepadApp) ensureCaret() {
	if a.caretLn < 0 {
		a.caretLn = 0
	}
	if a.caretLn >= len(a.lines) {
		a.caretLn = len(a.lines) - 1
	}
	line := []rune(a.lines[a.caretLn])
	if a.caretCol < 0 {
		a.caretCol = 0
	}
	if a.caretCol > len(line) {
		a.caretCol = len(line)
	}
	a.buildVis()
	row, _ := a.caretVisual()
	rows := a.textR.H / 16
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.vis)
	a.scroll.Visible = rows
	if row < a.scroll.Pos {
		a.scroll.Pos = row
	}
	if row >= a.scroll.Pos+rows {
		a.scroll.Pos = row - rows + 1
	}
	a.scroll.Clamp()
}

func (a *NotepadApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl {
		switch ev.Name {
		case "S":
			a.saveThen(func(bool) {})
			return true
		case "N":
			a.newDoc(d, w)
			return true
		case "O":
			a.openDialog(d)
			return true
		case "A":
			a.selAnchorLn, a.selAnchorCol = 0, 0
			a.caretLn = len(a.lines) - 1
			a.caretCol = len([]rune(a.lines[a.caretLn]))
			a.selActive = true
			a.clearSelIfEmpty()
			a.ensureCaret()
			d.Invalidate()
			return true
		case "C":
			a.copySelection()
			return true
		case "X":
			a.cutSelection()
			return true
		case "V":
			a.pasteFromClipboard()
			return true
		case "Home":
			a.selActive = false
			a.caretLn, a.caretCol = 0, 0
			a.ensureCaret()
			return true
		case "End":
			a.selActive = false
			a.caretLn = len(a.lines) - 1
			a.caretCol = len([]rune(a.lines[a.caretLn]))
			a.ensureCaret()
			return true
		}
		return false
	}

	if ev.Shift {
		switch ev.Name {
		case "Left", "Right", "Up", "Down", "Home", "End":
			if !a.selActive {
				a.selAnchorLn, a.selAnchorCol = a.caretLn, a.caretCol
			}
			switch ev.Name {
			case "Left":
				a.moveLeft()
			case "Right":
				a.moveRight()
			case "Up":
				a.moveVertical(-1)
			case "Down":
				a.moveVertical(1)
			case "Home":
				a.caretCol = 0
				a.ensureCaret()
			case "End":
				a.caretCol = len([]rune(a.lines[a.caretLn]))
				a.ensureCaret()
			}
			if a.caretLn == a.selAnchorLn && a.caretCol == a.selAnchorCol {
				a.selActive = false
			} else {
				a.selActive = true
			}
			d.Invalidate()
			return true
		}
	}

	switch ev.Name {
	case "Enter":
		a.deleteSelection()
		a.insertNewline()
	case "Backspace":
		if !a.deleteSelection() {
			a.backspace()
		}
	case "Delete":
		if !a.deleteSelection() {
			a.delete()
		}
	case "Tab":
		a.deleteSelection()
		a.insertText("    ")
	case "Left":
		a.selActive = false
		a.moveLeft()
	case "Right":
		a.selActive = false
		a.moveRight()
	case "Up":
		a.selActive = false
		a.moveVertical(-1)
	case "Down":
		a.selActive = false
		a.moveVertical(1)
	case "Home":
		a.selActive = false
		a.caretCol = 0
		a.ensureCaret()
	case "End":
		a.selActive = false
		a.caretCol = len([]rune(a.lines[a.caretLn]))
		a.ensureCaret()
	case "PgUp":
		rows := a.textR.H / 16
		a.scroll.ScrollBy(-rows)
		return true
	case "PgDn":
		rows := a.textR.H / 16
		a.scroll.ScrollBy(rows)
		return true
	case "F5":
		a.deleteSelection()
		a.insertText(time.Now().Format("2006-01-02 15:04"))
	default:
		if ev.Rune >= 32 {
			a.deleteSelection()
			a.insertText(string(ev.Rune))
		} else {
			return false
		}
	}
	a.ensureCaret()
	d.Invalidate()
	return true
}

func (a *NotepadApp) insertText(s string) {
	runes := []rune(a.lines[a.caretLn])
	ins := []rune(s)
	out := make([]rune, 0, len(runes)+len(ins))
	out = append(out, runes[:a.caretCol]...)
	out = append(out, ins...)
	out = append(out, runes[a.caretCol:]...)
	a.lines[a.caretLn] = string(out)
	a.caretCol += len(ins)
	a.modified = true
}

func (a *NotepadApp) insertNewline() {
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
}

func (a *NotepadApp) backspace() {
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

func (a *NotepadApp) delete() {
	runes := []rune(a.lines[a.caretLn])
	if a.caretCol < len(runes) {
		a.lines[a.caretLn] = string(append(runes[:a.caretCol], runes[a.caretCol+1:]...))
		a.modified = true
		return
	}
	if a.caretLn < len(a.lines)-1 {
		a.lines[a.caretLn] = a.lines[a.caretLn] + a.lines[a.caretLn+1]
		a.lines = append(a.lines[:a.caretLn+1], a.lines[a.caretLn+2:]...)
		a.modified = true
	}
}

func (a *NotepadApp) moveLeft() {
	if a.caretCol > 0 {
		a.caretCol--
	} else if a.caretLn > 0 {
		a.caretLn--
		a.caretCol = len([]rune(a.lines[a.caretLn]))
	}
	a.ensureCaret()
}

func (a *NotepadApp) moveRight() {
	if a.caretCol < len([]rune(a.lines[a.caretLn])) {
		a.caretCol++
	} else if a.caretLn < len(a.lines)-1 {
		a.caretLn++
		a.caretCol = 0
	}
	a.ensureCaret()
}

func (a *NotepadApp) moveVertical(dir int) {
	a.buildVis()
	row, x := a.caretVisual()
	row += dir
	if row < 0 || row >= len(a.vis) {
		return
	}
	v := a.vis[row]
	runes := []rune(a.lines[v.line])
	f := ui.DefaultFont()
	acc := 0
	col := v.start
	for col < v.end {
		rw := f.RuneWidth(runes[col])
		if acc+rw/2 > x {
			break
		}
		acc += rw
		col++
	}
	a.caretLn = v.line
	a.caretCol = col
	a.ensureCaret()
}

func (a *NotepadApp) openDialog(d *Desktop) {
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
			d.MessageBox("记事本", "无法打开文件：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		text := strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		a.lines = strings.Split(text, "\n")
		if len(a.lines) == 0 {
			a.lines = []string{""}
		}
		a.path = path
		a.modified = false
		a.caretLn, a.caretCol = 0, 0
		a.scroll.Pos = 0
		a.loadErr = ""
		a.selActive = false
		a.selecting = false
		d.Invalidate()
	})
}

func (a *NotepadApp) saveThen(done func(bool)) {
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
		a.d.MessageBox("记事本", "保存文件失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
		done(false)
		return
	}
	a.modified = false
	a.loadErr = ""
	a.d.Invalidate()
	done(true)
}

func (a *NotepadApp) saveAs(done func(bool)) {
	dir := "/root/文档"
	name := "无标题.txt"
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
			path += ".txt"
		}
		a.path = path
		a.saveThen(done)
	})
}

func (a *NotepadApp) newDoc(d *Desktop, w *Window) {
	if !a.modified {
		a.lines = []string{""}
		a.path = ""
		a.caretLn, a.caretCol = 0, 0
		a.scroll.Pos = 0
		a.loadErr = ""
		a.selActive = false
		a.selecting = false
		d.Invalidate()
		return
	}
	d.MessageBox("记事本", "文件已经修改，是否保存？", IconQuestion,
		[]string{"保存", "不保存", "取消"}, func(i int) {
			switch i {
			case 0:
				a.saveThen(func(ok bool) {
					if ok {
						a.lines = []string{""}
						a.path = ""
						a.caretLn, a.caretCol = 0, 0
						d.Invalidate()
					}
				})
			case 1:
				a.lines = []string{""}
				a.path = ""
				a.caretLn, a.caretCol = 0, 0
				a.modified = false
				a.loadErr = ""
				d.Invalidate()
			}
		})
}

func (a *NotepadApp) RequestClose(d *Desktop, w *Window) bool {
	if !a.modified {
		return true
	}
	d.MessageBox("记事本", "文件“"+strings.TrimSuffix(a.Title(), " - 记事本")+"”已经修改。\n\n是否保存更改？", IconQuestion,
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

func (a *NotepadApp) Close() {}

func (a *NotepadApp) loadSample() {
	a.SetContent("/root/文档/欢迎使用.txt", welcomeText)
}

const welcomeText = `欢迎使用 Windows 95 Linux！

这是一个运行在 ARM64 虚拟机上的 Win95 风格桌面环境。
它不需要 X11，直接把窗口画在 framebuffer 上。

您可以：
  * 使用“开始”菜单启动程序
  * 在终端中运行 Linux 命令
  * 用记事本编辑并保存文本文件
  * 用浏览器访问网页并下载文件

提示：按 Ctrl+S 保存文件，按 F5 插入当前时间。
`

func (a *NotepadApp) String() string { return a.Title() }
