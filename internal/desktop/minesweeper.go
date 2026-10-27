package desktop

import (
	"fmt"
	"math/rand"
	"time"

	"win95linux/internal/ui"
)

type mineLevel struct {
	name       string
	cols, rows int
	mines      int
}

var mineLevels = []mineLevel{
	{"初级", 9, 9, 10},
	{"中级", 16, 16, 40},
	{"高级", 30, 16, 99},
}

const (
	mineCell = 16
	mineTop  = 40
)

const (
	mineReady = iota
	minePlaying
	mineWon
	mineLost
)

// MineApp is the classic Minesweeper game.
type MineApp struct {
	Base
	d     *Desktop
	level int

	cols, rows, mines int
	mine              []bool
	open              []bool
	flag              []bool
	question          []bool
	number            []int8
	exploded          int

	state    int
	revealed int
	elapsed  int
	start    time.Time
	lastSec  int

	faceR    ui.Rect
	faceBtnR ui.Rect
	gridR    ui.Rect
	press    bool

	darkLine uint32
}

func NewMineApp() *MineApp                 { return newMineApp(0) }
func (a *MineApp) Title() string           { return "扫雷" }
func (a *MineApp) Icon() ui.IconKind       { return ui.IcoMine }
func (a *MineApp) Resizable() bool         { return false }
func (a *MineApp) DefaultSize() (int, int) { return a.windowSize() }
func (a *MineApp) MinSize() (int, int)     { return a.windowSize() }

func newMineApp(level int) *MineApp {
	if level < 0 || level >= len(mineLevels) {
		level = 0
	}
	lv := mineLevels[level]
	a := &MineApp{d: nil, level: level, cols: lv.cols, rows: lv.rows, mines: lv.mines}
	a.reset()
	return a
}

func (a *MineApp) windowSize() (int, int) {
	// DefaultSize is the outer window size, so it must include the frame,
	// title bar and menu bar (frame*2 + titleH + menuBarH = 52).
	return a.cols*mineCell + 18, a.rows*mineCell + 108
}

func (a *MineApp) reset() {
	n := a.cols * a.rows
	a.mine = make([]bool, n)
	a.open = make([]bool, n)
	a.flag = make([]bool, n)
	a.question = make([]bool, n)
	a.number = make([]int8, n)
	a.exploded = -1
	a.state = mineReady
	a.revealed = 0
	a.elapsed = 0
	a.lastSec = -1
}

func (a *MineApp) switchLevel(d *Desktop, w *Window, level int) {
	if level == a.level {
		a.reset()
		d.Invalidate()
		return
	}
	d.CloseWindowForced(w)
	app := newMineApp(level)
	app.d = d
	d.Open(app)
}

func (a *MineApp) Menus(d *Desktop, w *Window) []Menu {
	levelItems := make([]MenuItem, 0, len(mineLevels))
	for i, lv := range mineLevels {
		i, lv := i, lv
		levelItems = append(levelItems, MenuItem{
			Label:   fmt.Sprintf("%s（%d×%d，%d 颗雷）", lv.name, lv.cols, lv.rows, lv.mines),
			Checked: a.level == i,
			Enabled: true,
			Action:  func() { a.switchLevel(d, w, i) },
		})
	}
	items := []MenuItem{
		{Label: "新游戏", Accel: "F2", Enabled: true, Action: func() { a.reset(); d.Invalidate() }},
		{Separator: true},
	}
	items = append(items, levelItems...)
	items = append(items,
		MenuItem{Separator: true},
		MenuItem{Label: "退出", Enabled: true, Action: func() { d.CloseWindow(w) }},
	)
	return []Menu{
		{Label: "游戏(G)", Items: items},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于扫雷", Enabled: true, Action: func() {
				d.MessageBox("关于扫雷",
					"扫雷\n\n左键翻开方块，右键插旗。\n数字表示周围八格中的地雷数量。\n\n翻开全部安全方块即获胜。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *MineApp) Tick(d *Desktop, w *Window, now time.Time) {
	if a.state != minePlaying {
		return
	}
	sec := int(now.Sub(a.start).Seconds())
	if sec != a.lastSec {
		a.lastSec = sec
		a.elapsed = sec
		if sec > 999 {
			a.elapsed = 999
		}
		d.Invalidate()
	}
}

func (a *MineApp) neighborIndexes(i int) [8]int {
	col, row := i%a.cols, i/a.cols
	out := [8]int{-1, -1, -1, -1, -1, -1, -1, -1}
	k := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			c, r := col+dx, row+dy
			if c < 0 || c >= a.cols || r < 0 || r >= a.rows {
				continue
			}
			out[k] = r*a.cols + c
			k++
		}
	}
	return out
}

func (a *MineApp) placeMines(safe int) {
	n := a.cols * a.rows
	exclude := map[int]bool{safe: true}
	for _, i := range a.neighborIndexes(safe) {
		if i >= 0 {
			exclude[i] = true
		}
	}
	placed := 0
	for placed < a.mines {
		i := rand.Intn(n)
		if exclude[i] || a.mine[i] {
			continue
		}
		a.mine[i] = true
		placed++
	}
	a.recount()
}

func (a *MineApp) recount() {
	for i := 0; i < a.cols*a.rows; i++ {
		if a.mine[i] {
			continue
		}
		count := int8(0)
		for _, j := range a.neighborIndexes(i) {
			if j >= 0 && a.mine[j] {
				count++
			}
		}
		a.number[i] = count
	}
}

func (a *MineApp) reveal(i int) {
	if i < 0 || i >= len(a.open) || a.open[i] || a.flag[i] {
		return
	}
	if a.state == mineWon || a.state == mineLost {
		return
	}
	if a.state == mineReady {
		a.placeMines(i)
		a.state = minePlaying
		a.start = time.Now()
	}
	if a.mine[i] {
		a.state = mineLost
		a.exploded = i
		a.open[i] = true
		for j := range a.mine {
			if a.mine[j] {
				a.open[j] = true
			}
		}
		return
	}
	stack := []int{i}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur < 0 || a.open[cur] || a.flag[cur] || a.mine[cur] {
			continue
		}
		a.open[cur] = true
		a.revealed++
		if a.number[cur] == 0 {
			for _, j := range a.neighborIndexes(cur) {
				if j >= 0 && !a.open[j] && !a.flag[j] {
					stack = append(stack, j)
				}
			}
		}
	}
	if a.revealed == len(a.open)-a.mines {
		a.state = mineWon
		for j := range a.mine {
			if a.mine[j] {
				a.flag[j] = true
			}
		}
	}
}

func (a *MineApp) toggleFlag(i int) {
	if i < 0 || i >= len(a.open) || a.open[i] || a.state >= mineWon {
		return
	}
	if a.flag[i] {
		a.flag[i] = false
		a.question[i] = true
	} else if a.question[i] {
		a.question[i] = false
	} else {
		a.flag[i] = true
	}
}

func (a *MineApp) countFlags() int {
	n := 0
	for _, f := range a.flag {
		if f {
			n++
		}
	}
	return n
}

func (a *MineApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	a.faceR = ui.Rect{r.X + 6, r.Y + 5, r.W - 12, 30}
	c.Raised(a.faceR)

	counter := ui.Rect{a.faceR.X + 5, a.faceR.Y + 4, 42, 22}
	a.drawCounter(c, counter, a.mines-a.countFlags())
	a.drawCounter(c, ui.Rect{a.faceR.Right() - 47, counter.Y, 42, 22}, a.elapsed)

	btn := ui.Rect{a.faceR.X + a.faceR.W/2 - 15, a.faceR.Y + 2, 30, 26}
	a.faceBtnR = btn
	ui.Button(c, btn, "", a.press, true)
	a.drawFace(c, btn)

	gw, gh := a.cols*mineCell, a.rows*mineCell
	a.gridR = ui.Rect{r.X + (r.W-gw)/2, r.Y + mineTop + 6, gw, gh}
	bevel := ui.Rect{a.gridR.X - 2, a.gridR.Y - 2, gw + 4, gh + 4}
	c.FillRect(bevel, ui.ColFace)
	c.Sunken(bevel)

	f := ui.DefaultFont()
	for row := 0; row < a.rows; row++ {
		for col := 0; col < a.cols; col++ {
			i := row*a.cols + col
			x, y := a.gridR.X+col*mineCell, a.gridR.Y+row*mineCell
			cell := ui.Rect{x, y, mineCell, mineCell}
			if a.open[i] {
				c.FillRect(cell, ui.ColFace)
				c.HLine(x, y, mineCell, ui.ColShadow)
				c.VLine(x, y, mineCell, ui.ColShadow)
				switch {
				case a.mine[i]:
					a.drawMine(c, x, y, i == a.exploded)
				case a.number[i] > 0:
					label := fmt.Sprintf("%d", a.number[i])
					col := mineNumberColors[a.number[i]]
					f.Text(c, x+(mineCell-f.Width(label))/2, y, label, col)
				}
				continue
			}
			c.FillRect(cell, ui.ColFace)
			c.HLine(x, y, mineCell, ui.ColWhite)
			c.VLine(x, y, mineCell, ui.ColWhite)
			c.HLine(x, y+mineCell-1, mineCell, ui.ColShadow)
			c.VLine(x+mineCell-1, y, mineCell, ui.ColShadow)
			switch {
			case a.flag[i]:
				a.drawFlag(c, x, y)
			case a.question[i]:
				f.Text(c, x+(mineCell-f.Width("?"))/2, y, "?", ui.ColNavy)
			}
		}
	}
}

var mineNumberColors = []uint32{
	0, 0x0000A0, 0x008000, 0x800000, 0x000080,
	0x800040, 0x008080, 0x000000, 0x808080,
}

func (a *MineApp) drawCounter(c *ui.Canvas, r ui.Rect, value int) {
	c.FillRect(r, 0x000000)
	c.Sunken(r)
	if value < 0 {
		value = 0
	}
	if value > 999 {
		value = 999
	}
	text := fmt.Sprintf("%03d", value)
	f := ui.DefaultFont()
	f.Text(c, r.X+(r.W-f.Width(text))/2, r.Y+3, text, 0xFF2020)
}

func (a *MineApp) drawFace(c *ui.Canvas, btn ui.Rect) {
	cx, cy := btn.X+btn.W/2, btn.Y+btn.H/2
	for y := -9; y <= 9; y++ {
		for x := -9; x <= 9; x++ {
			if x*x+y*y > 81 {
				continue
			}
			col := uint32(0xF0D000)
			if x*x+y*y > 60 {
				col = 0xB08000
			}
			c.Set(cx+x, cy+y, col)
		}
	}
	mouth := 0
	switch a.state {
	case mineLost:
		mouth = 1
	case mineWon:
		mouth = 2
	}
	switch mouth {
	case 0:
		for _, p := range [][2]int{{cx - 4, cy - 4}, {cx + 4, cy - 4}} {
			c.Set(p[0], p[1], 0x000000)
			c.Set(p[0], p[1]+1, 0x000000)
		}
		for x := -4; x <= 4; x++ {
			c.Set(cx+x, cy+4+abs(x)/3, 0x000000)
		}
	case 1:
		for i := 0; i < 2; i++ {
			for _, p := range [][2]int{{cx - 5 + i, cy - 5 + i}, {cx - 3 + i, cy - 5 + i}, {cx - 5 + i, cy - 3 + i}, {cx - 3 + i, cy - 3 + i},
				{cx + 3 + i, cy - 5 + i}, {cx + 5 + i, cy - 5 + i}, {cx + 3 + i, cy - 3 + i}, {cx + 5 + i, cy - 3 + i}} {
				c.Set(p[0], p[1], 0x000000)
			}
		}
		for x := -4; x <= 4; x++ {
			c.Set(cx+x, cy+4-abs(x)/3, 0x000000)
		}
	case 2:
		for x := -6; x <= 6; x++ {
			c.Set(cx+x, cy-3, 0x000000)
			c.Set(cx+x, cy-4, 0x000000)
		}
		for x := -4; x <= 4; x++ {
			c.Set(cx+x, cy+4+abs(x)/3, 0x000000)
		}
	}
}

func (a *MineApp) drawFlag(c *ui.Canvas, x, y int) {
	for yy := 3; yy < 13; yy++ {
		c.Set(x+8, y+yy, 0x000000)
	}
	for i := 0; i < 5; i++ {
		for xx := 3; xx <= 8-i/2; xx++ {
			c.Set(x+xx, y+4+i, 0xC00000)
		}
	}
	for xx := 4; xx < 12; xx++ {
		c.Set(x+xx, y+13, 0x000000)
	}
}

func (a *MineApp) drawMine(c *ui.Canvas, x, y int, exploded bool) {
	bg := uint32(0)
	if exploded {
		bg = 0xC00000
		for yy := 0; yy < mineCell; yy++ {
			for xx := 0; xx < mineCell; xx++ {
				c.Set(x+xx, y+yy, bg)
			}
		}
	}
	cx, cy := x+8, y+8
	for yy := -4; yy <= 4; yy++ {
		for xx := -4; xx <= 4; xx++ {
			if xx*xx+yy*yy <= 16 {
				c.Set(cx+xx, cy+yy, 0x000000)
			}
		}
	}
	for i := 5; i <= 7; i++ {
		c.Set(cx, cy-i, 0x000000)
		c.Set(cx, cy+i, 0x000000)
		c.Set(cx-i, cy, 0x000000)
		c.Set(cx+i, cy, 0x000000)
		c.Set(cx+i-4, cy+i-4, 0x000000)
		c.Set(cx-i+4, cy+i-4, 0x000000)
	}
	c.Set(cx-2, cy-2, ui.ColWhite)
	c.Set(cx-1, cy-2, ui.ColWhite)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (a *MineApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	switch ev.Kind {
	case MouseDown:
		if a.faceBtnR.Contains(ev.X, ev.Y) {
			a.press = true
			a.reset()
			d.Invalidate()
			return
		}
		if !a.gridR.Contains(ev.X, ev.Y) {
			return
		}
		col := (ev.X - a.gridR.X) / mineCell
		row := (ev.Y - a.gridR.Y) / mineCell
		if col < 0 || col >= a.cols || row < 0 || row >= a.rows {
			return
		}
		i := row*a.cols + col
		if ev.Button == 3 {
			if a.state == mineReady || a.state == minePlaying {
				a.toggleFlag(i)
				d.Invalidate()
			}
			return
		}
		a.reveal(i)
		d.Invalidate()
	case MouseUp:
		if a.press {
			a.press = false
			d.Invalidate()
		}
	}
}

func (a *MineApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F2":
		a.reset()
		d.Invalidate()
		return true
	}
	return false
}
