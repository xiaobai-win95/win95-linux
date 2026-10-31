package desktop

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"win95linux/internal/ui"
)

const (
	solSpades = iota
	solHearts
	solDiamonds
	solClubs
)

type solCard struct {
	suit int
	rank int
	up   bool
}

func (c solCard) red() bool { return c.suit == solHearts || c.suit == solDiamonds }

func (c solCard) rankText() string {
	switch c.rank {
	case 1:
		return "A"
	case 11:
		return "J"
	case 12:
		return "Q"
	case 13:
		return "K"
	}
	return fmt.Sprintf("%d", c.rank)
}

type solSnapshot struct {
	stock, waste []solCard
	found        [4][]solCard
	tab          [7][]solCard
	score        int
}

type SolitaireApp struct {
	Base
	d   *Desktop
	win *Window

	stock, waste []solCard
	found        [4][]solCard
	tab          [7][]solCard
	score        int
	moves        int
	started      time.Time
	elapsed      int
	undo         []solSnapshot
	won          bool
	winShown     bool

	drag     []solCard
	dragCol  int
	dragSnap solSnapshot
	dragDX   int
	dragDY   int

	lastKey   string
	lastClick time.Time

	cardW, cardH, gap int
	topY, tabBottom   int
	stockR, wasteR    ui.Rect
	foundR            [4]ui.Rect
	tabR              [7]ui.Rect
	statusR           ui.Rect
}

func (d *Desktop) openSolitaire() {
	for _, w := range d.windows {
		if _, ok := w.App.(*SolitaireApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &SolitaireApp{d: d}
	a.newGame()
	d.Open(a)
}

func (a *SolitaireApp) Title() string     { return "纸牌" }
func (a *SolitaireApp) Icon() ui.IconKind { return ui.IcoCards }

func (a *SolitaireApp) DefaultSize() (int, int) { return 780, 560 }
func (a *SolitaireApp) MinSize() (int, int)     { return 560, 420 }
func (a *SolitaireApp) Resizable() bool         { return true }

func (a *SolitaireApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "游戏(G)", Items: []MenuItem{
			{Label: "发新牌(D)", Accel: "F2", Enabled: true, Action: func() { a.newGame() }},
			{Label: "撤销(U)", Accel: "Ctrl+Z", Enabled: len(a.undo) > 0, Action: func() { a.undoMove() }},
			{Separator: true},
			{Label: "退出(X)", Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于纸牌(A)", Action: func() {
				d.MessageBox("关于纸牌", "经典纸牌 (Klondike)\n\n把四种花色的牌从 A 到 K 依次移到右上角的接龙堆。\n\n· 拖动牌可以移动\n· 双击顶牌可自动送到接龙堆\n· 点牌堆发牌，发完后再点可重新发牌", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *SolitaireApp) newGame() {
	deck := make([]solCard, 0, 52)
	for suit := 0; suit < 4; suit++ {
		for rank := 1; rank <= 13; rank++ {
			deck = append(deck, solCard{suit: suit, rank: rank})
		}
	}
	rand.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	a.stock, a.waste = nil, nil
	for i := range a.found {
		a.found[i] = nil
	}
	for i := range a.tab {
		a.tab[i] = nil
	}
	idx := 0
	for col := 0; col < 7; col++ {
		for row := 0; row <= col; row++ {
			c := deck[idx]
			c.up = row == col
			a.tab[col] = append(a.tab[col], c)
			idx++
		}
	}
	a.stock = append(a.stock, deck[idx:]...)
	a.score, a.moves = 0, 0
	a.started = time.Now()
	a.elapsed = 0
	a.undo = nil
	a.won, a.winShown = false, false
	a.drag = nil
	a.lastKey = ""
	a.d.Invalidate()
}

func (a *SolitaireApp) snapshot() solSnapshot {
	var s solSnapshot
	s.stock = append([]solCard(nil), a.stock...)
	s.waste = append([]solCard(nil), a.waste...)
	for i := range a.found {
		s.found[i] = append([]solCard(nil), a.found[i]...)
	}
	for i := range a.tab {
		s.tab[i] = append([]solCard(nil), a.tab[i]...)
	}
	s.score = a.score
	return s
}

func (a *SolitaireApp) restore(s solSnapshot) {
	a.stock = append([]solCard(nil), s.stock...)
	a.waste = append([]solCard(nil), s.waste...)
	for i := range a.found {
		a.found[i] = append([]solCard(nil), s.found[i]...)
	}
	for i := range a.tab {
		a.tab[i] = append([]solCard(nil), s.tab[i]...)
	}
	a.score = s.score
	a.won = false
	a.winShown = false
}

func (a *SolitaireApp) pushUndo(s solSnapshot) {
	a.undo = append(a.undo, s)
	if len(a.undo) > 300 {
		a.undo = a.undo[len(a.undo)-300:]
	}
}

func (a *SolitaireApp) undoMove() {
	if a.drag != nil {
		a.restore(a.dragSnap)
		a.drag = nil
		a.d.Invalidate()
		return
	}
	if len(a.undo) == 0 {
		return
	}
	s := a.undo[len(a.undo)-1]
	a.undo = a.undo[:len(a.undo)-1]
	a.restore(s)
	if a.moves > 0 {
		a.moves--
	}
	a.d.Invalidate()
}

func (a *SolitaireApp) canStackTab(c solCard, dest []solCard) bool {
	if len(dest) == 0 {
		return c.rank == 13
	}
	top := dest[len(dest)-1]
	return top.up && top.rank == c.rank+1 && top.red() != c.red()
}

func (a *SolitaireApp) canStackFound(c solCard, pile []solCard) bool {
	if len(pile) == 0 {
		return c.rank == 1
	}
	top := pile[len(pile)-1]
	return top.suit == c.suit && c.rank == top.rank+1
}

func (a *SolitaireApp) movableSeq(col, idx int) []solCard {
	cards := a.tab[col]
	if idx < 0 || idx >= len(cards) || !cards[idx].up {
		return nil
	}
	for i := idx; i+1 < len(cards); i++ {
		if !cards[i+1].up || cards[i].rank != cards[i+1].rank+1 || cards[i].red() == cards[i+1].red() {
			return nil
		}
	}
	return append([]solCard(nil), cards[idx:]...)
}

func (a *SolitaireApp) flipExposed(col int) {
	if col < 0 || col > 6 {
		return
	}
	if n := len(a.tab[col]); n > 0 && !a.tab[col][n-1].up {
		a.tab[col][n-1].up = true
		a.score += 5
	}
}

func (a *SolitaireApp) checkWin() {
	if a.won {
		return
	}
	for _, pile := range a.found {
		if len(pile) != 13 {
			return
		}
	}
	a.won = true
	a.winShown = true
	a.d.Notify("纸牌", "恭喜，你赢了！")
	a.d.Post(func() {
		a.d.MessageBox("纸牌", fmt.Sprintf("恭喜！你完成了这一局！\n\n得分：%d\n用时：%s\n步数：%d\n\n再来一局吗？", a.score, solClock(a.elapsed), a.moves),
			IconInfo, []string{"是", "否"}, func(i int) {
				if i == 0 {
					a.newGame()
				}
			})
	})
}

func solClock(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func (a *SolitaireApp) Tick(d *Desktop, w *Window, now time.Time) {
	if a.started.IsZero() || a.won {
		return
	}
	sec := int(now.Sub(a.started).Seconds())
	if sec != a.elapsed {
		a.elapsed = sec
		d.Invalidate()
	}
}

func (a *SolitaireApp) layout(r ui.Rect) {
	margin, gap := 10, 10
	cardW := (r.W - 2*margin - 6*gap) / 7
	if cardW > 84 {
		cardW = 84
	}
	if cardW < 44 {
		gap = 6
		cardW = (r.W - 2*margin - 6*gap) / 7
		if cardW < 34 {
			cardW = 34
		}
	}
	cardH := cardW * 96 / 71
	totalW := 7*cardW + 6*gap
	x0 := r.X + (r.W-totalW)/2
	if x0 < r.X+4 {
		x0 = r.X + 4
	}
	a.cardW, a.cardH, a.gap = cardW, cardH, gap
	a.topY = r.Y + 8
	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	a.tabBottom = a.statusR.Y - 6
	for col := 0; col < 7; col++ {
		x := x0 + col*(cardW+gap)
		a.tabR[col] = ui.Rect{x, a.topY + cardH + 18, cardW, cardH}
	}
	a.stockR = ui.Rect{x0, a.topY, cardW, cardH}
	a.wasteR = ui.Rect{x0 + (cardW + gap), a.topY, cardW, cardH}
	for i := 0; i < 4; i++ {
		a.foundR[i] = ui.Rect{x0 + (3+i)*(cardW+gap), a.topY, cardW, cardH}
	}
}

func (a *SolitaireApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, 0x008000)
	a.layout(r)

	for i := 0; i < 4; i++ {
		a.drawSlot(c, a.foundR[i])
		if len(a.found[i]) > 0 {
			drawSolCard(c, ui.Rect{a.foundR[i].X, a.foundR[i].Y, a.cardW, a.cardH}, a.found[i][len(a.found[i])-1])
		}
	}
	if len(a.stock) > 0 {
		drawSolBack(c, ui.Rect{a.stockR.X, a.stockR.Y, a.cardW, a.cardH})
	} else {
		a.drawSlot(c, a.stockR)
		if len(a.waste) > 0 {
			a.drawRecycleArrow(c, a.stockR)
		}
	}
	if len(a.waste) > 0 {
		start := len(a.waste) - 3
		if start < 0 {
			start = 0
		}
		for i := start; i < len(a.waste); i++ {
			x := a.wasteR.X + (i-start)*a.gap
			drawSolCard(c, ui.Rect{x, a.wasteR.Y, a.cardW, a.cardH}, a.waste[i])
		}
	} else {
		a.drawSlot(c, a.wasteR)
	}

	for col := 0; col < 7; col++ {
		if len(a.tab[col]) == 0 {
			a.drawSlot(c, a.tabR[col])
			continue
		}
		ys := a.tabCardY(col)
		for i, cc := range a.tab[col] {
			rect := ui.Rect{a.tabR[col].X, a.tabR[col].Y + ys[i], a.cardW, a.cardH}
			if cc.up {
				drawSolCard(c, rect, cc)
			} else {
				drawSolBack(c, rect)
			}
		}
	}

	if len(a.drag) > 0 {
		x := d.MouseX - a.dragDX
		y := d.MouseY - a.dragDY
		for i, cc := range a.drag {
			rect := ui.Rect{x, y + i*18, a.cardW, a.cardH}
			c.FillRect(ui.Rect{rect.X + 3, rect.Y + 3, rect.W, rect.H}, 0x005000)
			drawSolCard(c, rect, cc)
		}
	}

	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	f := ui.DefaultFont()
	info := fmt.Sprintf("得分：%d　　时间：%s　　步数：%d", a.score, solClock(a.elapsed), a.moves)
	f.Text(c, a.statusR.X+8, a.statusR.Y+3, info, ui.ColText)
	hint := "双击顶牌可自动收牌"
	f.TextRight(c, a.statusR.Right()-8, a.statusR.Y+3, hint, ui.ColShadow)
}

func (a *SolitaireApp) drawSlot(c *ui.Canvas, r ui.Rect) {
	c.Frame(r, 0x00A000)
	c.Frame(ui.Rect{r.X + 1, r.Y + 1, r.W - 2, r.H - 2}, 0x006000)
}

func (a *SolitaireApp) drawRecycleArrow(c *ui.Canvas, r ui.Rect) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	radius := a.cardW / 5
	if radius < 8 {
		radius = 8
	}
	for deg := 30; deg < 340; deg += 8 {
		rad := float64(deg) * math.Pi / 180
		x := cx + int(float64(radius)*math.Cos(rad))
		y := cy + int(float64(radius)*math.Sin(rad))
		c.Set(x, y, 0x006000)
		c.Set(x+1, y, 0x006000)
	}
	tipRad := 340.0 * math.Pi / 180
	tipX := cx + int(float64(radius)*math.Cos(tipRad))
	tipY := cy + int(float64(radius)*math.Sin(tipRad))
	for i := 0; i < 5; i++ {
		c.Set(tipX-i, tipY+i, 0x006000)
		c.Set(tipX+2-i, tipY+i, 0x006000)
	}
}

func (a *SolitaireApp) tabCardY(col int) []int {
	cards := a.tab[col]
	down, up := 0, 0
	for _, c := range cards {
		if c.up {
			up++
		} else {
			down++
		}
	}
	base := a.tabR[col].Y
	maxOff := a.tabBottom - base - a.cardH
	if maxOff < 0 {
		maxOff = 0
	}
	downStep, upStep := 7, 21
	need := 0
	if down > 0 {
		need = (down - 1) * downStep
	}
	if up > 0 {
		need = down*downStep + (up-1)*upStep
	}
	if need > maxOff && need > 0 {
		downStep = downStep * maxOff / need
		upStep = upStep * maxOff / need
		if downStep < 2 {
			downStep = 2
		}
		if upStep < 6 {
			upStep = 6
		}
	}
	ys := make([]int, len(cards))
	off := 0
	for i := range cards {
		if i > 0 {
			if cards[i-1].up {
				off += upStep
			} else {
				off += downStep
			}
		}
		ys[i] = off
	}
	return ys
}

func (a *SolitaireApp) tableauHit(x, y int) (int, int, bool) {
	for col := 0; col < 7; col++ {
		if len(a.tab[col]) == 0 {
			continue
		}
		if x < a.tabR[col].X || x >= a.tabR[col].X+a.cardW {
			continue
		}
		ys := a.tabCardY(col)
		for i := len(a.tab[col]) - 1; i >= 0; i-- {
			top := a.tabR[col].Y + ys[i]
			if y >= top && y < top+a.cardH {
				return col, i, true
			}
		}
	}
	return 0, 0, false
}

func (a *SolitaireApp) foundationAt(x, y int) int {
	for i := 0; i < 4; i++ {
		if a.foundR[i].Contains(x, y) {
			return i
		}
	}
	return -1
}

func (a *SolitaireApp) columnAt(x, y int) int {
	for col := 0; col < 7; col++ {
		r := a.tabR[col]
		if x >= r.X && x < r.X+a.cardW && y >= r.Y {
			return col
		}
	}
	return -1
}

func (a *SolitaireApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel || ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		if a.won {
			return
		}
		if a.stockR.Contains(ev.X, ev.Y) {
			a.drawStock()
			return
		}
		if a.wasteR.Contains(ev.X, ev.Y) && len(a.waste) > 0 {
			top := a.waste[len(a.waste)-1]
			key := fmt.Sprintf("w/%d/%d/%d", len(a.waste), top.suit, top.rank)
			if a.doubleHit(key) {
				a.autoFromWaste()
				return
			}
			a.beginDrag(-1, len(a.waste)-1, ev)
			return
		}
		if col, idx, ok := a.tableauHit(ev.X, ev.Y); ok {
			cc := a.tab[col][idx]
			key := fmt.Sprintf("t/%d/%d/%d/%d", col, idx, cc.suit, cc.rank)
			if a.doubleHit(key) && idx == len(a.tab[col])-1 {
				a.autoFromTable(col)
				return
			}
			a.beginDrag(col, idx, ev)
		}
	case MouseMove:
		if len(a.drag) > 0 {
			d.Invalidate()
		}
	case MouseUp:
		if len(a.drag) > 0 {
			a.endDrag(ev)
		}
	}
}

func (a *SolitaireApp) doubleHit(key string) bool {
	now := time.Now()
	if key == a.lastKey && now.Sub(a.lastClick) < 450*time.Millisecond {
		a.lastKey = ""
		a.lastClick = time.Time{}
		return true
	}
	a.lastKey = key
	a.lastClick = now
	return false
}

func (a *SolitaireApp) beginDrag(col, idx int, ev MouseEvent) {
	var cards []solCard
	var rect ui.Rect
	if col < 0 {
		if len(a.waste) == 0 || idx != len(a.waste)-1 {
			return
		}
		start := len(a.waste) - 3
		if start < 0 {
			start = 0
		}
		cards = []solCard{a.waste[idx]}
		rect = ui.Rect{a.wasteR.X + (idx-start)*a.gap, a.wasteR.Y, a.cardW, a.cardH}
	} else {
		cards = a.movableSeq(col, idx)
		if len(cards) == 0 {
			return
		}
		ys := a.tabCardY(col)
		rect = ui.Rect{a.tabR[col].X, a.tabR[col].Y + ys[idx], a.cardW, a.cardH}
	}
	a.dragSnap = a.snapshot()
	a.drag = cards
	a.dragCol = col
	a.dragDX = ev.X - rect.X
	a.dragDY = ev.Y - rect.Y
	if col < 0 {
		a.waste = a.waste[:len(a.waste)-1]
	} else {
		a.tab[col] = a.tab[col][:idx]
	}
}

func (a *SolitaireApp) endDrag(ev MouseEvent) {
	cards := a.drag
	src := a.dragCol
	snap := a.dragSnap
	a.drag = nil
	if len(cards) == 0 {
		return
	}
	if fi := a.foundationAt(ev.X, ev.Y); fi >= 0 {
		if len(cards) == 1 && a.canStackFound(cards[0], a.found[fi]) {
			a.found[fi] = append(a.found[fi], cards[0])
			a.flipExposed(src)
			a.pushUndo(snap)
			a.score += 10
			a.moves++
			a.checkWin()
			a.d.Invalidate()
			return
		}
		a.restore(snap)
		a.d.Invalidate()
		return
	}
	col := a.columnAt(ev.X, ev.Y)
	if col >= 0 && a.canStackTab(cards[0], a.tab[col]) {
		if src == col && len(cards) > 0 {
			// Dropped back on the same pile: undo the lift quietly.
			a.restore(snap)
			a.d.Invalidate()
			return
		}
		a.tab[col] = append(a.tab[col], cards...)
		a.flipExposed(src)
		a.pushUndo(snap)
		if src < 0 {
			a.score += 5
		}
		a.moves++
		a.checkWin()
		a.d.Invalidate()
		return
	}
	a.restore(snap)
	a.d.Invalidate()
}

func (a *SolitaireApp) drawStock() {
	if a.won {
		return
	}
	if len(a.stock) == 0 && len(a.waste) == 0 {
		return
	}
	snap := a.snapshot()
	if len(a.stock) == 0 {
		for i := len(a.waste) - 1; i >= 0; i-- {
			cc := a.waste[i]
			cc.up = false
			a.stock = append(a.stock, cc)
		}
		a.waste = nil
		a.score -= 100
		if a.score < 0 {
			a.score = 0
		}
	} else {
		n := 3
		if n > len(a.stock) {
			n = len(a.stock)
		}
		for i := 0; i < n; i++ {
			cc := a.stock[len(a.stock)-1]
			a.stock = a.stock[:len(a.stock)-1]
			cc.up = true
			a.waste = append(a.waste, cc)
		}
	}
	a.pushUndo(snap)
	a.moves++
	a.d.Invalidate()
}

func (a *SolitaireApp) autoFromWaste() {
	if len(a.waste) == 0 {
		return
	}
	cc := a.waste[len(a.waste)-1]
	for fi := 0; fi < 4; fi++ {
		if !a.canStackFound(cc, a.found[fi]) {
			continue
		}
		snap := a.snapshot()
		a.waste = a.waste[:len(a.waste)-1]
		a.found[fi] = append(a.found[fi], cc)
		a.pushUndo(snap)
		a.score += 10
		a.moves++
		a.checkWin()
		a.d.Invalidate()
		return
	}
}

func (a *SolitaireApp) autoFromTable(col int) {
	if col < 0 || len(a.tab[col]) == 0 {
		return
	}
	cc := a.tab[col][len(a.tab[col])-1]
	for fi := 0; fi < 4; fi++ {
		if !a.canStackFound(cc, a.found[fi]) {
			continue
		}
		snap := a.snapshot()
		a.tab[col] = a.tab[col][:len(a.tab[col])-1]
		a.found[fi] = append(a.found[fi], cc)
		a.flipExposed(col)
		a.pushUndo(snap)
		a.score += 10
		a.moves++
		a.checkWin()
		a.d.Invalidate()
		return
	}
}

func (a *SolitaireApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F2":
		a.newGame()
		return true
	case "Esc":
		if len(a.drag) > 0 {
			a.restore(a.dragSnap)
			a.drag = nil
			d.Invalidate()
			return true
		}
	}
	if ev.Ctrl && ev.Name == "Z" {
		a.undoMove()
		return true
	}
	return false
}

func (a *SolitaireApp) Close() {}

func drawSolCard(c *ui.Canvas, r ui.Rect, cc solCard) {
	c.FillRect(r, ui.ColWhite)
	c.Frame(r, 0x404040)
	c.HLine(r.X+1, r.Y+1, r.W-2, ui.ColWhite)
	f := ui.DefaultFont()
	col := uint32(0x000000)
	if cc.red() {
		col = 0xC00000
	}
	f.Text(c, r.X+4, r.Y+3, cc.rankText(), col)
	suitSize := r.W / 7
	if suitSize < 4 {
		suitSize = 4
	}
	if suitSize > 7 {
		suitSize = 7
	}
	if r.W >= 52 {
		drawSolSuit(c, cc.suit, r.X+4, r.Y+20, suitSize, col, false)
	}
	big := r.W / 4
	if big < 9 {
		big = 9
	}
	if big > 15 {
		big = 15
	}
	cx := r.X + r.W/2
	cy := r.Y + r.H/2 + r.H/10
	drawSolSuit(c, cc.suit, cx-big/2, cy-big/2, big, col, true)
}

func drawSolBack(c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColWhite)
	c.Frame(r, 0x404040)
	inner := ui.Rect{r.X + 2, r.Y + 2, r.W - 4, r.H - 4}
	c.FillRect(inner, 0x000080)
	c.Frame(inner, 0xFFFFFF)
	pat := ui.Rect{r.X + 4, r.Y + 4, r.W - 8, r.H - 8}
	for y := pat.Y; y < pat.Bottom(); y++ {
		for x := pat.X; x < pat.Right(); x++ {
			if (x+y)%6 == 0 {
				c.Set(x, y, 0x5A7AD0)
			}
		}
	}
}

// drawSolSuit renders a filled suit glyph. The large variant is a 11x11
// bitmap; the small one is 5x5 so it stays legible in a card corner.
func drawSolSuit(c *ui.Canvas, suit, x, y, size int, col uint32, large bool) {
	var pat []string
	if large {
		switch suit {
		case solHearts:
			pat = []string{
				".XX...XX...",
				"XXXX.XXXX..",
				"XXXXXXXXXXX",
				"XXXXXXXXXXX",
				"XXXXXXXXXXX",
				".XXXXXXXXX.",
				"..XXXXXXX..",
				"...XXXXX...",
				"....XXX....",
				".....X.....",
			}
		case solDiamonds:
			pat = []string{
				".....X.....",
				"....XXX....",
				"...XXXXX...",
				"..XXXXXXX..",
				".XXXXXXXXX.",
				"XXXXXXXXXXX",
				".XXXXXXXXX.",
				"..XXXXXXX..",
				"...XXXXX...",
				"....XXX....",
				".....X.....",
			}
		case solSpades:
			pat = []string{
				".....X.....",
				"....XXX....",
				"...XXXXX...",
				"..XXXXXXX..",
				".XXXXXXXXX.",
				"XXXXXXXXXXX",
				"XXXXXXXXXXX",
				"..XXXXXXX..",
				".....X.....",
				"....XXX....",
			}
		default:
			pat = []string{
				"...XXXXX...",
				"..XXXXXXX..",
				"..XXXXXXX..",
				"...XXXXX...",
				".XX.XXX.XX.",
				"XXXXXXXXXXX",
				"XXXXXXXXXXX",
				".XXXXXXXXX.",
				"...XXXXX...",
				".....X.....",
				"....XXX....",
			}
		}
	} else {
		switch suit {
		case solHearts:
			pat = []string{".X.X.", "XXXXX", "XXXXX", ".XXX.", "..X.."}
		case solDiamonds:
			pat = []string{"..X..", ".XXX.", "XXXXX", ".XXX.", "..X.."}
		case solSpades:
			pat = []string{"..X..", ".XXX.", "XXXXX", "..X..", ".XXX."}
		default:
			pat = []string{"..X..", ".XXX.", "..X..", "X.X.X", "..X.."}
		}
	}
	scale := size / len(pat[0])
	if scale < 1 {
		scale = 1
	}
	w := len(pat[0]) * scale
	h := len(pat) * scale
	ox := x + (size-w)/2
	oy := y + (size-h)/2
	for row, line := range pat {
		for ci, ch := range line {
			if ch != 'X' {
				continue
			}
			c.FillRect(ui.Rect{ox + ci*scale, oy + row*scale, scale, scale}, col)
		}
	}
}
