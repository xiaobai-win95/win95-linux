package desktop

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"win95linux/internal/ui"
)

type termCell struct {
	r    rune
	fg   uint32
	bg   uint32
	bold bool
	wide bool
	cont bool
}

type termScreen struct {
	mu       sync.Mutex
	cols     int
	rows     int
	screen   [][]termCell
	back     [][]termCell
	cx, cy   int
	fg, bg   uint32
	bold     bool
	wrapNext bool
	view     int
	cursor   bool
	savedX   int
	savedY   int
	proc     *termProc
	inputCh  chan []byte
	done     chan struct{}
	closed   bool
	err      string
	scroll   VScroll
	onOutput func()
}

type TerminalApp struct {
	Base
	d     *Desktop
	win   *Window
	t     *termScreen
	area  ui.Rect
	press string

	selAnchorX, selAnchorY int
	selCurX, selCurY       int
	selecting              bool
	selActive              bool
}

var ansiColors = []uint32{
	0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xC0C0C0,
	0x808080, 0xFF0000, 0x00FF00, 0xFFFF00, 0x0000FF, 0xFF00FF, 0x00FFFF, 0xFFFFFF,
}

func NewTerminalApp(d *Desktop) *TerminalApp {
	a := &TerminalApp{d: d}
	a.t = &termScreen{
		cols: 80, rows: 24,
		fg:      0xC0C0C0,
		bg:      0x000000,
		cursor:  true,
		inputCh: make(chan []byte, 64),
		done:    make(chan struct{}),
	}
	a.t.onOutput = a.invalidate
	a.t.resetGrid()
	a.t.start()
	return a
}

func (a *TerminalApp) Title() string           { return "终端" }
func (a *TerminalApp) Icon() ui.IconKind       { return ui.IcoTerminal }
func (a *TerminalApp) DefaultSize() (int, int) { return 760, 500 }
func (a *TerminalApp) MinSize() (int, int)     { return 320, 220 }
func (a *TerminalApp) Resizable() bool         { return true }

func (a *TerminalApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "新建终端(N)", Enabled: true, Action: func() { d.openTerminal() }},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "复制(C)", Accel: "Ctrl+Shift+C", Enabled: a.selActive, Action: func() {
				if s := a.selectedText(); s != "" {
					clipboardWrite(s)
				}
			}},
			{Label: "粘贴(P)", Accel: "Ctrl+Shift+V", Enabled: clipboardRead() != "", Action: func() {
				s := clipboardRead()
				if s != "" {
					a.send([]byte(strings.ReplaceAll(s, "\n", "\r")))
				}
			}},
			{Separator: true},
			{Label: "清屏(L)", Enabled: true, Action: func() { a.clear() }},
			{Separator: true},
			{Label: "发送 Ctrl+C", Enabled: true, Action: func() { a.send([]byte{3}) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于终端(A)", Enabled: true, Action: func() {
				d.MessageBox("关于终端", "Windows 95 Linux 终端\n版本 1.0\n\n基于 Linux 伪终端与 BusyBox ash。\n支持滚动、ANSI 颜色与常用控制键。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func blankCells(n int, bg uint32) []termCell {
	line := make([]termCell, n)
	for i := range line {
		line[i] = termCell{r: ' ', bg: bg}
	}
	return line
}

func (t *termScreen) resetGrid() {
	t.screen = make([][]termCell, t.rows)
	for i := range t.screen {
		t.screen[i] = blankCells(t.cols, t.bg)
	}
	t.back = nil
	t.cx, t.cy = 0, 0
	t.view = 0
	t.wrapNext = false
}

func (t *termScreen) start() {
	proc, err := startTerminal(t.cols, t.rows)
	if err != nil {
		t.err = err.Error()
		t.writeText("无法启动终端：" + err.Error() + "\r\n")
		return
	}
	t.proc = proc
	go t.readLoop()
	go t.writeLoop()
}

func (t *termScreen) writeText(s string) {
	for _, r := range s {
		if r == '\n' {
			t.linefeed()
		} else if r == '\r' {
			t.cx = 0
		} else {
			t.put(r)
		}
	}
}

func (t *termScreen) readLoop() {
	buf := make([]byte, 8192)
	for {
		n, err := t.proc.read(buf)
		if n > 0 {
			t.mu.Lock()
			t.feed(buf[:n])
			t.view = 0
			t.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				t.mu.Lock()
				if !t.closed {
					t.writeText("\r\n[pty 读取结束: " + err.Error() + "]\r\n")
				}
				t.mu.Unlock()
			}
			t.mu.Lock()
			if !t.closed && t.err == "" {
				t.writeText("\r\n[进程已退出]\r\n")
			}
			t.mu.Unlock()
			t.notify()
			return
		}
		t.notify()
	}
}

func (t *termScreen) notify() {
	if t.onOutput != nil {
		t.onOutput()
	}
}

func (t *termScreen) writeLoop() {
	for {
		select {
		case <-t.done:
			return
		case b := <-t.inputCh:
			if t.proc != nil && len(b) > 0 {
				t.proc.write(b)
			}
		}
	}
}

func (t *termScreen) close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()
	close(t.done)
	if t.proc != nil {
		t.proc.close()
	}
}

func (t *termScreen) send(b []byte) {
	if t.closed {
		return
	}
	select {
	case t.inputCh <- b:
	default:
	}
}

func (t *termScreen) put(r rune) {
	if r == '\t' {
		next := (t.cx/8 + 1) * 8
		for t.cx < next && t.cx < t.cols {
			t.putCell(' ')
		}
		return
	}
	w := runeCellWidth(r)
	if w == 0 {
		return
	}
	if t.wrapNext {
		t.cx = 0
		t.linefeed()
	}
	if w == 2 && t.cx >= t.cols-1 {
		t.cx = 0
		t.linefeed()
	}
	line := t.screen[t.cy]
	c := termCell{r: r, fg: t.fg, bg: t.bg, bold: t.bold, wide: w == 2}
	line[t.cx] = c
	if w == 2 && t.cx+1 < t.cols {
		line[t.cx+1] = termCell{r: ' ', bg: t.bg, cont: true}
	}
	t.cx += w
	if t.cx >= t.cols {
		t.cx = t.cols - 1
		t.wrapNext = true
	} else {
		t.wrapNext = false
	}
}

func (t *termScreen) putCell(r rune) {
	if t.cx >= t.cols {
		t.cx = 0
		t.linefeed()
	}
	t.screen[t.cy][t.cx] = termCell{r: r, fg: t.fg, bg: t.bg, bold: t.bold}
	t.cx++
	if t.cx >= t.cols {
		t.cx = t.cols - 1
		t.wrapNext = true
	}
}

func (t *termScreen) linefeed() {
	t.wrapNext = false
	if t.cy >= t.rows-1 {
		t.back = append(t.back, t.screen[0])
		if len(t.back) > 1200 {
			t.back = t.back[len(t.back)-1200:]
		}
		copy(t.screen, t.screen[1:])
		t.screen[t.rows-1] = blankCells(t.cols, t.bg)
	} else {
		t.cy++
	}
}

func (t *termScreen) feed(data []byte) {
	i := 0
	for i < len(data) {
		b := data[i]
		if b == 0x1b {
			consumed := t.parseEscape(data[i:])
			if consumed <= 0 {
				i++
			} else {
				i += consumed
			}
			continue
		}
		switch b {
		case '\n':
			t.linefeed()
			i++
		case '\r':
			t.cx = 0
			t.wrapNext = false
			i++
		case '\b':
			if t.cx > 0 {
				t.cx--
			}
			i++
		case 0x07:
			i++
		case 0x0e, 0x0f:
			i++
		default:
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size <= 1 {
				i++
				continue
			}
			if r < 32 {
				i++
				continue
			}
			t.put(r)
			i += size
		}
	}
}

func (t *termScreen) parseEscape(data []byte) int {
	if len(data) < 2 {
		return len(data)
	}
	switch data[1] {
	case '[':
		j := 2
		for j < len(data) {
			if data[j] >= 0x40 && data[j] <= 0x7e {
				t.csi(string(data[2:j]), string(data[j]))
				return j + 1
			}
			j++
		}
		return len(data)
	case ']':
		j := 2
		for j < len(data) {
			if data[j] == 0x07 {
				return j + 1
			}
			if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return len(data)
	case '7':
		t.savedX, t.savedY = t.cx, t.cy
		return 2
	case '8':
		t.cx, t.cy = t.savedX, t.savedY
		return 2
	case 'D':
		t.linefeed()
		return 2
	case 'M':
		if t.cy > 0 {
			t.cy--
		}
		return 2
	case 'E':
		t.cx = 0
		t.linefeed()
		return 2
	case 'c':
		t.resetGrid()
		return 2
	case '(', ')', '#':
		if len(data) >= 3 {
			return 3
		}
		return len(data)
	}
	return 2
}

func atoiDefault(s string, d int) int {
	if s == "" {
		return d
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return n
}

func (t *termScreen) csi(param, final string) {
	private := strings.HasPrefix(param, "?")
	if private {
		param = param[1:]
	}
	parts := strings.Split(param, ";")
	n := atoiDefault(parts[0], 1)
	switch final {
	case "A":
		t.cy -= n
		if t.cy < 0 {
			t.cy = 0
		}
		t.wrapNext = false
	case "B":
		t.cy += n
		if t.cy >= t.rows {
			t.cy = t.rows - 1
		}
		t.wrapNext = false
	case "C":
		t.cx += n
		if t.cx >= t.cols {
			t.cx = t.cols - 1
		}
		t.wrapNext = false
	case "D":
		t.cx -= n
		if t.cx < 0 {
			t.cx = 0
		}
		t.wrapNext = false
	case "E":
		t.cy += n
		t.cx = 0
		if t.cy >= t.rows {
			t.cy = t.rows - 1
		}
	case "F":
		t.cy -= n
		t.cx = 0
		if t.cy < 0 {
			t.cy = 0
		}
	case "G", "`":
		t.cx = n - 1
		if t.cx < 0 {
			t.cx = 0
		}
		if t.cx >= t.cols {
			t.cx = t.cols - 1
		}
	case "d":
		t.cy = n - 1
		if t.cy < 0 {
			t.cy = 0
		}
		if t.cy >= t.rows {
			t.cy = t.rows - 1
		}
	case "H", "f":
		row := atoiDefault(parts[0], 1)
		col := 1
		if len(parts) > 1 {
			col = atoiDefault(parts[1], 1)
		}
		t.cy = row - 1
		t.cx = col - 1
		if t.cy < 0 {
			t.cy = 0
		}
		if t.cy >= t.rows {
			t.cy = t.rows - 1
		}
		if t.cx < 0 {
			t.cx = 0
		}
		if t.cx >= t.cols {
			t.cx = t.cols - 1
		}
		t.wrapNext = false
	case "J":
		mode := atoiDefault(parts[0], 0)
		if mode == 2 || mode == 3 {
			for i := range t.screen {
				t.screen[i] = blankCells(t.cols, t.bg)
			}
			if mode == 3 {
				t.back = nil
			}
		} else if mode == 1 {
			for y := 0; y <= t.cy && y < t.rows; y++ {
				end := t.cols
				if y == t.cy {
					end = t.cx + 1
				}
				for x := 0; x < end; x++ {
					t.screen[y][x] = termCell{r: ' ', bg: t.bg}
				}
			}
		} else {
			for x := t.cx; x < t.cols; x++ {
				t.screen[t.cy][x] = termCell{r: ' ', bg: t.bg}
			}
			for y := t.cy + 1; y < t.rows; y++ {
				t.screen[y] = blankCells(t.cols, t.bg)
			}
		}
	case "K":
		mode := atoiDefault(parts[0], 0)
		line := t.screen[t.cy]
		switch mode {
		case 1:
			for x := 0; x <= t.cx && x < t.cols; x++ {
				line[x] = termCell{r: ' ', bg: t.bg}
			}
		case 2:
			t.screen[t.cy] = blankCells(t.cols, t.bg)
		default:
			for x := t.cx; x < t.cols; x++ {
				line[x] = termCell{r: ' ', bg: t.bg}
			}
		}
	case "P":
		line := t.screen[t.cy]
		if n < 1 {
			n = 1
		}
		if t.cx+n <= t.cols {
			copy(line[t.cx:], line[t.cx+n:])
			for x := t.cols - n; x < t.cols; x++ {
				line[x] = termCell{r: ' ', bg: t.bg}
			}
		}
	case "@":
		line := t.screen[t.cy]
		if t.cx+n <= t.cols {
			copy(line[t.cx+n:], line[t.cx:])
			for x := t.cx; x < t.cx+n; x++ {
				line[x] = termCell{r: ' ', bg: t.bg}
			}
		}
	case "X":
		for x := t.cx; x < t.cx+n && x < t.cols; x++ {
			t.screen[t.cy][x] = termCell{r: ' ', bg: t.bg}
		}
	case "L":
		for k := 0; k < n; k++ {
			t.screen = append(t.screen, nil)
			copy(t.screen[t.cy+1:], t.screen[t.cy:])
			t.screen[t.cy] = blankCells(t.cols, t.bg)
			t.screen = t.screen[:t.rows]
		}
	case "M":
		for k := 0; k < n; k++ {
			copy(t.screen[t.cy:], t.screen[t.cy+1:])
			t.screen[t.rows-1] = blankCells(t.cols, t.bg)
		}
	case "m":
		t.sgr(parts, param)
	case "h", "l":
		if private && param == "25" {
			t.cursor = final == "h"
		}
	case "r", "s", "u", "n", "t":
		// Scroll regions and status reports are not needed by the shell.
	}
}

func (t *termScreen) sgr(parts []string, raw string) {
	if raw == "" {
		t.fg, t.bg, t.bold = 0xC0C0C0, 0x000000, false
		return
	}
	for i := 0; i < len(parts); i++ {
		n := atoiDefault(parts[i], 0)
		switch {
		case n == 0:
			t.fg, t.bg, t.bold = 0xC0C0C0, 0x000000, false
		case n == 1:
			t.bold = true
		case n == 22:
			t.bold = false
		case n >= 30 && n <= 37:
			t.fg = ansiColors[n-30]
		case n >= 90 && n <= 97:
			t.fg = ansiColors[8+n-90]
		case n == 39:
			t.fg = 0xC0C0C0
		case n >= 40 && n <= 47:
			t.bg = ansiColors[n-40]
		case n >= 100 && n <= 107:
			t.bg = ansiColors[8+n-100]
		case n == 49:
			t.bg = 0x000000
		case n == 38 || n == 48:
			var col uint32
			ok := false
			if i+1 < len(parts) && parts[i+1] == "5" && i+2 < len(parts) {
				idx := atoiDefault(parts[i+2], 0)
				col = color256(idx)
				ok = true
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" && i+4 < len(parts) {
				r := atoiDefault(parts[i+2], 0)
				g := atoiDefault(parts[i+3], 0)
				b := atoiDefault(parts[i+4], 0)
				col = uint32(r)<<16 | uint32(g)<<8 | uint32(b)
				ok = true
				i += 4
			}
			if ok {
				if n == 38 {
					t.fg = col
				} else {
					t.bg = col
				}
			}
		}
	}
}

func color256(n int) uint32 {
	if n < 0 {
		n = 0
	}
	if n < 16 {
		return ansiColors[n]
	}
	if n < 232 {
		levels := []uint32{0, 95, 135, 175, 215, 255}
		n -= 16
		r := levels[(n/36)%6]
		g := levels[(n/6)%6]
		b := levels[n%6]
		return r<<16 | g<<8 | b
	}
	if n > 255 {
		n = 255
	}
	g := uint32(8 + (n-232)*10)
	return g<<16 | g<<8 | g
}

func (t *termScreen) resize(cols, rows int) {
	if cols < 20 {
		cols = 20
	}
	if rows < 6 {
		rows = 6
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if cols == t.cols && rows == t.rows {
		return
	}
	old := t.screen
	oldRows, oldCols := t.rows, t.cols
	t.cols, t.rows = cols, rows
	t.screen = make([][]termCell, rows)
	for y := 0; y < rows; y++ {
		line := blankCells(cols, t.bg)
		if y < oldRows && y < len(old) {
			copy(line, old[y])
			if oldCols > cols && y == t.cy {
				t.cx = cols - 1
			}
		}
		t.screen[y] = line
	}
	if t.cy >= rows {
		t.cy = rows - 1
	}
	if t.cx >= cols {
		t.cx = cols - 1
	}
	t.view = 0
	t.wrapNext = false
	if t.proc != nil {
		t.proc.resize(cols, rows)
	}
}

func runeCellWidth(r rune) int {
	if r < 0x1100 {
		if r == 0 {
			return 0
		}
		return 1
	}
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func (a *TerminalApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, 0x000000)
	a.area = client
	scrollW := 16
	screenR := ui.Rect{client.X + 2, client.Y + 2, client.W - scrollW - 4, client.H - 4}
	t := a.t
	t.mu.Lock()
	cols := screenR.W / 8
	rows := screenR.H / 16
	t.mu.Unlock()
	if cols != t.cols || rows != t.rows {
		t.resize(cols, rows)
	}

	f := ui.DefaultFont()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scroll.Total = len(t.back) + t.rows
	t.scroll.Visible = t.rows
	t.scroll.Pos = len(t.back) - t.view
	t.scroll.Clamp()
	sel1x, sel1y, sel2x, sel2y, hasSel := a.selRange(t.cols, t.rows)
	hasSel = hasSel && t.view == 0
	if hasSel {
		if sel1x > 0 && sel1x < len(t.screen[sel1y]) && t.screen[sel1y][sel1x].cont {
			sel1x--
		}
		if sel2x > 0 && sel2x < len(t.screen[sel2y]) && t.screen[sel2y][sel2x].cont {
			sel2x--
		}
	}
	for row := 0; row < t.rows; row++ {
		var line []termCell
		if t.view > 0 && row < t.view {
			idx := len(t.back) - t.view + row
			if idx >= 0 && idx < len(t.back) {
				line = t.back[idx]
			}
		} else {
			idx := row - t.view
			if idx >= 0 && idx < len(t.screen) {
				line = t.screen[idx]
			}
		}
		if line == nil {
			continue
		}
		y := screenR.Y + row*16
		x := screenR.X
		for col := 0; col < t.cols && col < len(line); col++ {
			cell := line[col]
			cw := 8
			if cell.cont {
				continue
			}
			if cell.wide {
				cw = 16
			}
			selected := false
			if hasSel && row >= sel1y && row <= sel2y {
				from, to := 0, t.cols-1
				if row == sel1y {
					from = sel1x
				}
				if row == sel2y {
					to = sel2x
				}
				selected = col >= from && col <= to
			}
			c.FillRect(ui.Rect{x, y, cw, 16}, cell.bg)
			if selected {
				c.FillRect(ui.Rect{x, y, cw, 16}, ui.ColNavy)
			}
			fg := cell.fg
			if selected {
				fg = ui.ColWhite
			}
			if cell.r != 0 && cell.r != ' ' {
				f.Text(c, x, y, string(cell.r), fg)
				if cell.bold {
					f.Text(c, x+1, y, string(cell.r), fg)
				}
			}
			x += cw
		}
	}
	if t.cursor && t.view == 0 && d.Active() == w && !a.selActive {
		cx := screenR.X + t.cx*8
		cy := screenR.Y + t.cy*16
		c.FillRect(ui.Rect{cx, cy, 8, 16}, 0xC0C0C0)
		cell := t.screen[t.cy][t.cx]
		if cell.r != 0 && cell.r != ' ' {
			f.Text(c, cx, cy, string(cell.r), 0x000000)
		}
	}
	t.scroll.Draw(c, ui.Rect{client.Right() - scrollW - 1, client.Y + 2, scrollW, client.H - 4})

	if t.err != "" {
		f.TextClip(c, client.X+6, client.Bottom()-20, client.W-28, t.err, 0xFF8080)
	}
}

func (a *TerminalApp) clear() {
	t := a.t
	t.mu.Lock()
	for i := range t.screen {
		t.screen[i] = blankCells(t.cols, t.bg)
	}
	t.cx, t.cy = 0, 0
	t.view = 0
	t.mu.Unlock()
	a.d.Invalidate()
}

func (a *TerminalApp) send(b []byte) {
	a.t.send(b)
}

func (a *TerminalApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	t := a.t
	scrollW := 16
	screenR := ui.Rect{client.X + 2, client.Y + 2, client.W - scrollW - 4, client.H - 4}
	t.mu.Lock()
	scrollRect := ui.Rect{client.Right() - 17, client.Y + 2, 16, client.H - 4}
	changed, _ := t.scroll.Mouse(ev, scrollRect)
	if changed {
		t.view = len(t.back) - t.scroll.Pos
		if t.view < 0 {
			t.view = 0
		}
		if t.view > len(t.back) {
			t.view = len(t.back)
		}
	}
	view, cols, rows := t.view, t.cols, t.rows
	t.mu.Unlock()
	if ev.Kind == MouseWheel {
		t.mu.Lock()
		t.view -= ev.Wheel
		if t.view < 0 {
			t.view = 0
		}
		if t.view > len(t.back) {
			t.view = len(t.back)
		}
		t.mu.Unlock()
		a.selActive = false
		a.selecting = false
		d.Invalidate()
		return
	}
	if ev.Kind == MouseMove && a.selecting {
		a.selExtend(screenR, cols, rows, ev.X, ev.Y)
		d.Invalidate()
		return
	}
	if ev.Kind == MouseDown && ev.Button == 1 && view == 0 && screenR.Contains(ev.X, ev.Y) {
		x, y := a.cellAt(screenR, cols, rows, ev.X, ev.Y)
		a.selAnchorX, a.selAnchorY = x, y
		a.selCurX, a.selCurY = x, y
		a.selecting = true
		a.selActive = false
		d.Invalidate()
		return
	}
	if ev.Kind == MouseUp && a.selecting {
		a.selecting = false
		a.clearSelIfEmpty()
		d.Invalidate()
		return
	}
	if changed {
		d.Invalidate()
	}
}

func (a *TerminalApp) cellAt(r ui.Rect, cols, rows, x, y int) (int, int) {
	cx := (x - r.X) / 8
	cy := (y - r.Y) / 16
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cx >= cols {
		cx = cols - 1
	}
	if cy >= rows {
		cy = rows - 1
	}
	return cx, cy
}

func (a *TerminalApp) selExtend(r ui.Rect, cols, rows, x, y int) {
	a.selCurX, a.selCurY = a.cellAt(r, cols, rows, x, y)
	a.selActive = a.selCurX != a.selAnchorX || a.selCurY != a.selAnchorY
}

func (a *TerminalApp) selRange(cols, rows int) (int, int, int, int, bool) {
	if !a.selActive {
		return 0, 0, 0, 0, false
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	x1, y1 := a.selAnchorX, a.selAnchorY
	x2, y2 := a.selCurX, a.selCurY
	if x1 < 0 {
		x1 = 0
	}
	if x2 < 0 {
		x2 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if y2 < 0 {
		y2 = 0
	}
	if x1 >= cols {
		x1 = cols - 1
	}
	if x2 >= cols {
		x2 = cols - 1
	}
	if y1 >= rows {
		y1 = rows - 1
	}
	if y2 >= rows {
		y2 = rows - 1
	}
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		x1, y1, x2, y2 = x2, y2, x1, y1
	}
	if y1 == y2 && x1 == x2 {
		return 0, 0, 0, 0, false
	}
	return x1, y1, x2, y2, true
}

func (a *TerminalApp) clearSelIfEmpty() {
	if _, _, _, _, ok := a.selRange(a.t.cols, a.t.rows); !ok {
		a.selActive = false
	}
}

func (a *TerminalApp) selectedText() string {
	t := a.t
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.view != 0 {
		return ""
	}
	x1, y1, x2, y2, ok := a.selRange(t.cols, t.rows)
	if !ok {
		return ""
	}
	if x1 > 0 && y1 < len(t.screen) && x1 < len(t.screen[y1]) && t.screen[y1][x1].cont {
		x1--
	}
	if x2 > 0 && y2 < len(t.screen) && x2 < len(t.screen[y2]) && t.screen[y2][x2].cont {
		x2--
	}
	var b strings.Builder
	for y := y1; y <= y2 && y < len(t.screen); y++ {
		from, to := 0, t.cols-1
		if y == y1 {
			from = x1
		}
		if y == y2 {
			to = x2
		}
		line := t.screen[y]
		if to >= len(line) {
			to = len(line) - 1
		}
		var row strings.Builder
		for x := from; x <= to; x++ {
			cell := line[x]
			if cell.cont {
				continue
			}
			if cell.r == 0 {
				row.WriteByte(' ')
			} else {
				row.WriteRune(cell.r)
			}
		}
		b.WriteString(strings.TrimRight(row.String(), " "))
		if y < y2 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (a *TerminalApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl && ev.Shift {
		switch strings.ToUpper(ev.Name) {
		case "C":
			if s := a.selectedText(); s != "" {
				clipboardWrite(s)
			}
			return true
		case "V":
			s := clipboardRead()
			if s != "" {
				a.send([]byte(strings.ReplaceAll(s, "\n", "\r")))
			}
			return true
		}
	}
	if ev.Ctrl {
		name := ev.Name
		if name == "" && ev.Rune != 0 {
			name = strings.ToUpper(string(ev.Rune))
		}
		if len(name) == 1 {
			ch := name[0]
			if ch >= 'A' && ch <= 'Z' {
				a.send([]byte{ch - 'A' + 1})
				return true
			}
			switch ch {
			case '[':
				a.send([]byte{0x1b})
				return true
			case '\\':
				a.send([]byte{0x1c})
				return true
			case ']':
				a.send([]byte{0x1d})
				return true
			}
		}
	}
	switch ev.Name {
	case "Enter":
		a.send([]byte{'\r'})
	case "Backspace":
		a.send([]byte{0x7f})
	case "Tab":
		a.send([]byte{'\t'})
	case "Esc":
		a.send([]byte{0x1b})
	case "Up":
		a.send([]byte("\x1b[A"))
	case "Down":
		a.send([]byte("\x1b[B"))
	case "Right":
		a.send([]byte("\x1b[C"))
	case "Left":
		a.send([]byte("\x1b[D"))
	case "Home":
		a.send([]byte("\x1b[H"))
	case "End":
		a.send([]byte("\x1b[F"))
	case "Delete":
		a.send([]byte("\x1b[3~"))
	case "Insert":
		a.send([]byte("\x1b[2~"))
	case "PgUp":
		a.t.mu.Lock()
		a.t.view += a.t.rows
		if a.t.view > len(a.t.back) {
			a.t.view = len(a.t.back)
		}
		a.t.mu.Unlock()
	case "PgDn":
		a.t.mu.Lock()
		a.t.view -= a.t.rows
		if a.t.view < 0 {
			a.t.view = 0
		}
		a.t.mu.Unlock()
	case "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12":
		n, _ := strconv.Atoi(ev.Name[1:])
		a.send([]byte(fmt.Sprintf("\x1b[%d~", 10+n)))
	default:
		if ev.Rune != 0 && !ev.Ctrl && !ev.Alt {
			a.send([]byte(string(ev.Rune)))
		} else {
			return false
		}
	}
	return true
}

func (a *TerminalApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	cols := (client.W - 20) / 8
	rows := (client.H - 4) / 16
	a.t.resize(cols, rows)
}

func (a *TerminalApp) Close() {
	a.t.close()
}

// SeedText is used by the offscreen preview to show realistic shell output.
func (a *TerminalApp) SeedText(s string) {
	t := a.t
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeText(s)
	t.view = 0
}

func (a *TerminalApp) invalidate() {
	if a.d != nil {
		a.d.Invalidate()
	}
}
