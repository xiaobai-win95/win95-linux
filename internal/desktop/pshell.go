package desktop

import (
	"fmt"
	"sort"
	"strings"

	"win95linux/internal/ui"
)

// PShellApp is the Windows PowerShell console application. It shares the
// cmdlet/pipeline engine with the ISE panel through psSession.
type PShellApp struct {
	Base
	d    *Desktop
	win  *Window
	sess *psSession

	lines []psLine
	input []rune
	caret int

	hist    []string
	histIdx int
	draft   string

	scroll VScroll
	stick  bool
	busy   bool
	status string
	closed bool

	textR   ui.Rect
	statusR ui.Rect
	press   string
}

func NewPShellApp(d *Desktop) *PShellApp {
	a := &PShellApp{
		d:       d,
		sess:    newPSSession(),
		histIdx: 0,
		stick:   true,
	}
	a.lines = []psLine{
		{text: "Windows PowerShell 26.2", col: psColPrompt, bold: true},
		{text: "版权所有 (C) 2026 Windows 95 Linux 项目。保留所有权利。", col: psColDim},
		{text: ""},
		{text: "正在尝试新的跨平台 PowerShell：本控制台内置常用 cmdlet，", col: psColDim},
		{text: "未识别的命令会交给 /bin/sh 执行。输入 help 查看命令列表。", col: psColDim},
		{text: ""},
	}
	return a
}

func (a *PShellApp) Title() string {
	if a.busy {
		return "Windows PowerShell - 正在执行…"
	}
	return "Windows PowerShell"
}

func (a *PShellApp) Icon() ui.IconKind       { return ui.IcoShell }
func (a *PShellApp) DefaultSize() (int, int) { return 820, 540 }
func (a *PShellApp) MinSize() (int, int)     { return 420, 280 }
func (a *PShellApp) Resizable() bool         { return true }

func (a *PShellApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "新建窗口(N)", Enabled: true, Action: func() { d.openPowerShell() }},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "复制全部输出(C)", Accel: "Ctrl+Shift+C", Enabled: len(a.lines) > 0, Action: func() { a.copyAll() }},
			{Label: "粘贴(P)", Accel: "Ctrl+V", Enabled: clipboardRead() != "", Action: func() { a.paste() }},
			{Separator: true},
			{Label: "清屏(L)", Accel: "Ctrl+L", Enabled: true, Action: func() { a.clearScreen() }},
		}},
		{Label: "视图(V)", Items: []MenuItem{
			{Label: "滚动到顶部(T)", Accel: "Ctrl+Home", Enabled: true, Action: func() { a.scrollToTop() }},
			{Label: "滚动到底部(B)", Accel: "Ctrl+End", Enabled: true, Action: func() { a.scrollToBottom() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "命令帮助(H)", Accel: "F1", Enabled: true, Action: func() { a.runLine("help") }},
			{Separator: true},
			{Label: "关于 PowerShell(A)", Enabled: true, Action: func() {
				d.MessageBox("关于 Windows PowerShell",
					"Windows PowerShell 26.2\nWindows 95 Linux 中文版\n\n内置 cmdlet 子集与管道支持。\n未识别的命令通过 /bin/sh 执行。\n\nCtrl+Alt+K 可打开屏幕键盘。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *PShellApp) layout(client ui.Rect) {
	a.statusR = ui.Rect{client.X, client.Bottom() - 20, client.W, 20}
	console := ui.Rect{client.X, client.Y, client.W, client.H - 20}
	a.textR = ui.Rect{console.X + 4, console.Y + 4, console.W - 16 - 8, console.H - 8}
	if a.textR.W < 16 {
		a.textR.W = 16
	}
	if a.textR.H < 16 {
		a.textR.H = 16
	}
}

type psSeg struct {
	text string
	col  uint32
	bold bool
}

type psRow struct {
	segs []psSeg
}

func (r psRow) plain() string {
	var b strings.Builder
	for _, s := range r.segs {
		b.WriteString(s.text)
	}
	return b.String()
}

func psWrapSpans(text string, width int) [][2]int {
	f := ui.DefaultFont()
	runes := []rune(text)
	var spans [][2]int
	start, w := 0, 0
	for i, r := range runes {
		rw := f.RuneWidth(r)
		if i > start && w+rw > width {
			spans = append(spans, [2]int{start, i})
			start, w = i, 0
		}
		w += rw
	}
	spans = append(spans, [2]int{start, len(runes)})
	return spans
}

// buildRows lays out all output plus the current prompt line. It returns the
// visual rows, the index of the caret row and the caret x offset inside it.
func (a *PShellApp) buildRows(width int) (rows []psRow, caretRow, caretX int) {
	if width < 8 {
		width = 8
	}
	for _, ln := range a.lines {
		text := ln.text
		col := ln.col
		if col == 0 {
			col = psColText
		}
		if text == "" {
			rows = append(rows, psRow{})
			continue
		}
		for _, sp := range psWrapSpans(text, width) {
			runes := []rune(text)
			rows = append(rows, psRow{segs: []psSeg{{text: string(runes[sp[0]:sp[1]]), col: col, bold: ln.bold}}})
		}
	}
	caretRow, caretX = -1, 0
	if a.busy {
		return rows, -1, 0
	}
	prompt := a.sess.Prompt()
	text := prompt + string(a.input)
	plen := len([]rune(prompt))
	caretPos := plen + a.caret
	f := ui.DefaultFont()
	for _, sp := range psWrapSpans(text, width) {
		runes := []rune(text)
		row := psRow{}
		if sp[0] < plen {
			end := sp[1]
			if end > plen {
				end = plen
			}
			row.segs = append(row.segs, psSeg{text: string(runes[sp[0]:end]), col: psColPrompt, bold: true})
			if sp[1] > plen {
				row.segs = append(row.segs, psSeg{text: string(runes[plen:sp[1]]), col: psColText})
			}
		} else {
			row.segs = append(row.segs, psSeg{text: string(runes[sp[0]:sp[1]]), col: psColText})
		}
		if caretPos >= sp[0] && caretPos <= sp[1] {
			caretRow = len(rows)
			x := 0
			for i := sp[0]; i < caretPos; i++ {
				x += f.RuneWidth(runes[i])
			}
			caretX = x
		}
		rows = append(rows, row)
	}
	return rows, caretRow, caretX
}

func (a *PShellApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, ui.ColFace)
	a.layout(client)
	console := ui.Rect{client.X, client.Y, client.W, client.H - 20}
	c.FillRect(console, psColBg)

	f := ui.DefaultFont()
	rows, caretRow, caretX := a.buildRows(a.textR.W)
	a.scroll.Total = len(rows)
	a.scroll.Visible = a.textR.H / 16
	if a.scroll.Visible < 1 {
		a.scroll.Visible = 1
	}
	if a.stick {
		a.scroll.Pos = a.scroll.Max()
	}
	a.scroll.Clamp()

	for i := 0; i < a.scroll.Visible; i++ {
		idx := a.scroll.Pos + i
		if idx >= len(rows) {
			break
		}
		y := a.textR.Y + i*16
		x := a.textR.X
		for _, seg := range rows[idx].segs {
			if seg.bold {
				f.TextBold(c, x, y, seg.text, seg.col)
			} else {
				f.Text(c, x, y, seg.text, seg.col)
			}
			x += f.Width(seg.text)
		}
	}

	if caretRow >= 0 && d.Active() == w && a.scroll.Pos == a.scroll.Max() {
		visible := caretRow - a.scroll.Pos
		if visible >= 0 && visible < a.scroll.Visible {
			cx := a.textR.X + caretX
			cy := a.textR.Y + visible*16
			c.FillRect(ui.Rect{cx, cy, 8, 16}, psColText)
			if a.caret < len(a.input) {
				f.Text(c, cx, cy, string(a.input[a.caret]), psColBg)
			}
		}
	}

	sr := ui.Rect{console.Right() - 17, console.Y + 2, 16, console.H - 4}
	a.scroll.Draw(c, sr)

	// Status bar.
	st := a.statusR
	c.FillRect(st, ui.ColFace)
	c.HLine(st.X, st.Y, st.W, ui.ColShadow)
	c.HLine(st.X, st.Y+1, st.W, ui.ColWhite)
	status := a.status
	if status == "" {
		status = "就绪"
	}
	f.Text(c, st.X+6, st.Y+3, status, ui.ColText)
	right := psDisplayPath(a.sess.getCwd())
	f.TextClip(c, st.X+st.W/2, st.Y+3, st.W/2-8, right, ui.ColShadow)
}

func (a *PShellApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	sr := ui.Rect{client.Right() - 17, client.Y + 2, 16, client.H - 24}
	changed, consumed := a.scroll.Mouse(ev, sr)
	if changed {
		a.stick = a.scroll.Pos >= a.scroll.Max()
		d.Invalidate()
	}
	if consumed {
		return
	}
	if ev.Kind == MouseWheel {
		a.scroll.ScrollBy(-ev.Wheel * 3)
		a.stick = a.scroll.Pos >= a.scroll.Max()
		d.Invalidate()
	}
}

func (a *PShellApp) scrollToTop() {
	a.scroll.Pos = 0
	a.stick = false
	a.d.Invalidate()
}

func (a *PShellApp) scrollToBottom() {
	a.scroll.Pos = a.scroll.Max()
	a.stick = true
	a.d.Invalidate()
}

func (a *PShellApp) copyAll() {
	var b strings.Builder
	for _, ln := range a.lines {
		b.WriteString(ln.text)
		b.WriteByte('\n')
	}
	if !a.busy {
		b.WriteString(a.sess.Prompt())
		b.WriteString(string(a.input))
	}
	if s := b.String(); s != "" {
		clipboardWrite(strings.TrimRight(s, "\n"))
		a.status = "已复制输出到剪贴板"
		a.d.Invalidate()
	}
}

func (a *PShellApp) paste() {
	s := clipboardRead()
	if s == "" || a.busy {
		return
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	a.insertText(s)
	a.stick = true
	a.d.Invalidate()
}

func (a *PShellApp) clearScreen() {
	a.lines = nil
	a.scroll.Pos = 0
	a.stick = true
	a.d.Invalidate()
}

func (a *PShellApp) insertText(s string) {
	runes := []rune(s)
	out := make([]rune, 0, len(a.input)+len(runes))
	out = append(out, a.input[:a.caret]...)
	out = append(out, runes...)
	out = append(out, a.input[a.caret:]...)
	a.input = out
	a.caret += len(runes)
}

func (a *PShellApp) runLine(line string) {
	if a.busy {
		return
	}
	a.input = []rune(line)
	a.caret = len(a.input)
	a.submit()
}

func (a *PShellApp) submit() {
	if a.busy {
		return
	}
	line := strings.TrimSpace(string(a.input))
	prompt := a.sess.Prompt()
	a.lines = append(a.lines, psLine{text: prompt + string(a.input), col: psColDim})
	a.input = nil
	a.caret = 0
	a.stick = true
	if line != "" {
		if len(a.hist) == 0 || a.hist[len(a.hist)-1] != line {
			a.hist = append(a.hist, line)
			if len(a.hist) > 100 {
				a.hist = a.hist[len(a.hist)-100:]
			}
		}
	}
	a.histIdx = len(a.hist)
	a.draft = ""
	if line == "" {
		a.d.Invalidate()
		return
	}
	a.busy = true
	a.status = "正在执行…"
	a.d.Invalidate()
	go func() {
		lines, clear, exit := a.sess.Execute(line)
		a.d.Post(func() {
			if a.closed {
				return
			}
			if clear {
				a.lines = nil
			}
			a.lines = append(a.lines, lines...)
			a.busy = false
			a.status = ""
			a.stick = true
			a.d.Invalidate()
			if exit && a.win != nil {
				a.d.CloseWindowForced(a.win)
			}
		})
	}()
}

func (a *PShellApp) complete() {
	text := string(a.input[:a.caret])
	start := strings.LastIndexAny(text, " \t")
	if start < 0 {
		start = 0
	} else {
		start++
	}
	word := text[start:]
	var candidates []string
	if start == 0 {
		seen := map[string]bool{}
		add := func(name string) {
			lower := strings.ToLower(name)
			if seen[lower] || !strings.HasPrefix(lower, strings.ToLower(word)) {
				return
			}
			seen[lower] = true
			candidates = append(candidates, name)
		}
		for _, c := range psCmdlets {
			add(c.name)
		}
		for alias := range psAliases {
			add(alias)
		}
	}
	if len(candidates) == 0 {
		return
	}
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i]) < strings.ToLower(candidates[j])
	})
	common := candidates[0]
	for _, c := range candidates[1:] {
		for !strings.HasPrefix(strings.ToLower(c), strings.ToLower(common)) && common != "" {
			common = common[:len(common)-1]
		}
	}
	if len(candidates) == 1 {
		insert := candidates[0][len(word):] + " "
		a.insertText(insert)
	} else if len(common) > len(word) {
		a.insertText(common[len(word):])
	} else {
		var cols []string
		for _, c := range candidates {
			cols = append(cols, psPadRight(c, 18))
		}
		for i := 0; i < len(cols); i += 4 {
			end := i + 4
			if end > len(cols) {
				end = len(cols)
			}
			a.lines = append(a.lines, psLine{text: "  " + strings.Join(cols[i:end], ""), col: psColInfo})
		}
	}
	a.stick = true
	a.d.Invalidate()
}

func (a *PShellApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.busy {
		if ev.Ctrl && strings.EqualFold(ev.Name, "C") {
			a.sess.cancelCurrent()
			a.status = "正在取消…"
			a.d.Invalidate()
			return true
		}
		switch ev.Name {
		case "PgUp":
			a.scroll.ScrollBy(-a.scroll.Visible)
			a.stick = false
			a.d.Invalidate()
		case "PgDn":
			a.scroll.ScrollBy(a.scroll.Visible)
			a.stick = a.scroll.Pos >= a.scroll.Max()
			a.d.Invalidate()
		}
		return true
	}
	if ev.Ctrl && ev.Shift {
		switch strings.ToUpper(ev.Name) {
		case "C":
			a.copyAll()
			return true
		}
	}
	if ev.Ctrl {
		switch strings.ToUpper(ev.Name) {
		case "L":
			a.clearScreen()
			return true
		case "V":
			a.paste()
			return true
		case "C":
			a.lines = append(a.lines, psLine{text: a.sess.Prompt() + string(a.input) + "^C", col: psColDim})
			a.input = nil
			a.caret = 0
			a.stick = true
			a.d.Invalidate()
			return true
		case "A":
			a.caret = 0
			a.d.Invalidate()
			return true
		case "E":
			a.caret = len(a.input)
			a.d.Invalidate()
			return true
		case "HOME":
			a.scrollToTop()
			return true
		case "END":
			a.scrollToBottom()
			return true
		}
	}
	switch ev.Name {
	case "Enter":
		a.submit()
		return true
	case "Tab":
		a.complete()
		return true
	case "Backspace":
		if a.caret > 0 {
			a.input = append(a.input[:a.caret-1], a.input[a.caret:]...)
			a.caret--
			a.stick = true
			a.d.Invalidate()
		}
		return true
	case "Delete":
		if a.caret < len(a.input) {
			a.input = append(a.input[:a.caret], a.input[a.caret+1:]...)
			a.d.Invalidate()
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
			a.d.Invalidate()
		}
		return true
	case "Right":
		if a.caret < len(a.input) {
			a.caret++
			a.d.Invalidate()
		}
		return true
	case "Home":
		a.caret = 0
		a.d.Invalidate()
		return true
	case "End":
		a.caret = len(a.input)
		a.d.Invalidate()
		return true
	case "Up":
		if len(a.hist) > 0 {
			if a.histIdx == len(a.hist) {
				a.draft = string(a.input)
			}
			if a.histIdx > 0 {
				a.histIdx--
			}
			a.input = []rune(a.hist[a.histIdx])
			a.caret = len(a.input)
			a.stick = true
			a.d.Invalidate()
		}
		return true
	case "Down":
		if a.histIdx < len(a.hist) {
			a.histIdx++
			if a.histIdx == len(a.hist) {
				a.input = []rune(a.draft)
			} else {
				a.input = []rune(a.hist[a.histIdx])
			}
			a.caret = len(a.input)
			a.stick = true
			a.d.Invalidate()
		}
		return true
	case "PgUp":
		a.scroll.ScrollBy(-a.scroll.Visible)
		a.stick = false
		a.d.Invalidate()
		return true
	case "PgDn":
		a.scroll.ScrollBy(a.scroll.Visible)
		a.stick = a.scroll.Pos >= a.scroll.Max()
		a.d.Invalidate()
		return true
	case "F1":
		a.runLine("help")
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.insertText(string(ev.Rune))
		a.stick = true
		a.d.Invalidate()
		return true
	}
	return false
}

func (a *PShellApp) Close() {
	a.closed = true
	a.sess.cancelCurrent()
}

// SeedText is used by the offscreen preview to show realistic console output.
func (a *PShellApp) SeedText(s string) {
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		a.lines = append(a.lines, psLine{text: line, col: psColText})
	}
	a.stick = true
}

func (a *PShellApp) String() string { return fmt.Sprintf("PowerShell(%s)", a.sess.getCwd()) }
