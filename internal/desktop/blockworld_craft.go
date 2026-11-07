package desktop

import (
	"fmt"

	"win95linux/internal/ui"
)

// bwFloatText is a short rising label (pickup counts, crafting results) drawn
// over the world for a bit of animation feedback.
type bwFloatText struct {
	x, y float64
	text string
	col  uint32
	life float64
}

func (a *BlockWorldApp) addFloat(tx, ty int, text string, col uint32) {
	a.floats = append(a.floats, bwFloatText{
		x: float64(tx * bwTile), y: float64(ty*bwTile) - 6,
		text: text, col: col, life: 1.1,
	})
	if len(a.floats) > 40 {
		a.floats = a.floats[len(a.floats)-40:]
	}
}

func (a *BlockWorldApp) stepFloats(dt float64) {
	out := a.floats[:0]
	for _, ft := range a.floats {
		ft.life -= dt
		if ft.life <= 0 {
			continue
		}
		ft.y -= 28 * dt
		out = append(out, ft)
	}
	a.floats = out
}

func (a *BlockWorldApp) drawFloats(c *ui.Canvas, client ui.Rect, f *ui.Font) {
	for _, ft := range a.floats {
		sx := client.X + int(ft.x) - a.camX
		sy := client.Y + int(ft.y) - a.camY
		if sx < client.X-60 || sx > client.Right() || sy < client.Y || sy > client.Bottom() {
			continue
		}
		col := ft.col
		if ft.life < 0.4 {
			col = bwLerp(col, 0x606060, 1-ft.life/0.4)
		}
		f.TextBold(c, sx, sy, ft.text, col)
	}
}

// bwRecipe converts inventory items into a product. Advanced recipes need a
// placed crafting table within reach; basic ones work anywhere.
type bwRecipe struct {
	Name      string
	In        []bwSlot
	Out       bwSlot
	NeedTable bool
}

var bwRecipes = []bwRecipe{
	{"木板", []bwSlot{{bwWood, 1}}, bwSlot{bwPlanks, 4}, false},
	{"工作台", []bwSlot{{bwPlanks, 4}}, bwSlot{bwCraftingTable, 1}, false},
	{"圆石", []bwSlot{{bwStone, 1}}, bwSlot{bwCobble, 1}, false},
	{"玻璃", []bwSlot{{bwSand, 2}}, bwSlot{bwGlass, 2}, true},
	{"砖块", []bwSlot{{bwCobble, 4}}, bwSlot{bwBrick, 4}, true},
}

func (a *BlockWorldApp) hotbarCountOf(b bwBlock) int {
	n := 0
	for _, s := range a.hotbar {
		if s.Block == b {
			n += s.Count
		}
	}
	return n
}

func (a *BlockWorldApp) hasRoomFor(b bwBlock, n int) bool {
	capacity := 0
	for _, s := range a.hotbar {
		if s.Count == 0 {
			capacity += 99
		} else if s.Block == b {
			capacity += 99 - s.Count
		}
	}
	return capacity >= n
}

func (a *BlockWorldApp) nearTable() bool {
	cx := int((a.px + bwPW/2) / bwTile)
	cy := int((a.py + bwPH/2) / bwTile)
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			if a.world.At(cx+dx, cy+dy) == bwCraftingTable {
				return true
			}
		}
	}
	return false
}

func (a *BlockWorldApp) canCraft(i int) bool {
	if i < 0 || i >= len(bwRecipes) {
		return false
	}
	r := bwRecipes[i]
	if r.NeedTable && !a.nearTable() {
		return false
	}
	for _, in := range r.In {
		if a.hotbarCountOf(in.Block) < in.Count {
			return false
		}
	}
	return a.hasRoomFor(r.Out.Block, r.Out.Count)
}

func (a *BlockWorldApp) removeItems(b bwBlock, n int) {
	for i := range a.hotbar {
		if n == 0 {
			break
		}
		s := &a.hotbar[i]
		if s.Block == b && s.Count > 0 {
			take := min(s.Count, n)
			s.Count -= take
			n -= take
			if s.Count == 0 {
				s.Block = bwAir
			}
		}
	}
}

func (a *BlockWorldApp) addItemN(b bwBlock, n int) {
	for i := 0; i < n; i++ {
		a.addItem(b)
	}
}

func (a *BlockWorldApp) doCraft(i int) {
	if i < 0 || i >= len(bwRecipes) {
		return
	}
	r := bwRecipes[i]
	if !a.canCraft(i) {
		if r.NeedTable && !a.nearTable() {
			a.setStatus("这个配方需要站在放置的工作台旁")
		} else {
			a.setStatus("材料不足或背包已满，无法合成" + r.Name)
		}
		bwSound("deny")
		return
	}
	for _, in := range r.In {
		a.removeItems(in.Block, in.Count)
	}
	a.addItemN(r.Out.Block, r.Out.Count)
	a.dirty = true
	a.setStatus(fmt.Sprintf("合成了 %s ×%d", bwNames[r.Out.Block], r.Out.Count))
	a.addFloat(int(a.px/bwTile), int(a.py/bwTile)-1,
		fmt.Sprintf("+%d %s", r.Out.Count, bwNames[r.Out.Block]), 0x80F080)
	bwSound("craft")
}

func (a *BlockWorldApp) toggleCraft(d *Desktop) {
	a.craftOpen = !a.craftOpen
	a.mining = false
	a.mineProg = 0
	if d != nil {
		d.Invalidate()
	}
}

func (a *BlockWorldApp) toggle3D(d *Desktop) {
	a.view3D = !a.view3D
	if a.view3D {
		a.setStatus("3D 立体视角已开启（按 V 切回平面）")
	} else {
		a.setStatus("已切回平面视角")
	}
	if d != nil {
		d.Invalidate()
	}
}

func (a *BlockWorldApp) craftKey(d *Desktop, ev KeyEvent) bool {
	n := len(bwRecipes)
	switch ev.Name {
	case "Esc", "E":
		a.toggleCraft(d)
	case "Up":
		a.craftSel = (a.craftSel - 1 + n) % n
	case "Down":
		a.craftSel = (a.craftSel + 1) % n
	case "Enter":
		a.doCraft(a.craftSel)
	}
	if ev.Rune == 'c' || ev.Rune == 'C' {
		a.doCraft(a.craftSel)
	}
	if d != nil {
		d.Invalidate()
	}
	return true
}

func craftPanelRect(client ui.Rect) ui.Rect {
	w, h := 440, 70+len(bwRecipes)*40+46
	if w > client.W-20 {
		w = client.W - 20
	}
	x := client.X + (client.W-w)/2
	y := client.Y + (client.H-h)/2
	if y < client.Y+4 {
		y = client.Y + 4
	}
	return ui.Rect{x, y, w, h}
}

func craftRowRect(client ui.Rect, i int) ui.Rect {
	p := craftPanelRect(client)
	return ui.Rect{p.X + 10, p.Y + 56 + i*40, p.W - 20, 36}
}

func craftButtons(client ui.Rect) (ui.Rect, ui.Rect) {
	p := craftPanelRect(client)
	by := p.Bottom() - 38
	return ui.Rect{p.X + p.W/2 - 110, by, 100, 26},
		ui.Rect{p.X + p.W/2 + 10, by, 100, 26}
}

func (a *BlockWorldApp) craftMouse(d *Desktop, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		if ev.Wheel != 0 {
			n := len(bwRecipes)
			a.craftSel = (a.craftSel - ev.Wheel%n + n) % n
			d.Invalidate()
		}
		return
	}
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	if !craftPanelRect(client).Contains(ev.X, ev.Y) {
		a.toggleCraft(d)
		return
	}
	for i := range bwRecipes {
		if craftRowRect(client, i).Contains(ev.X, ev.Y) {
			a.craftSel = i
			d.Invalidate()
			return
		}
	}
	craftBtn, closeBtn := craftButtons(client)
	if craftBtn.Contains(ev.X, ev.Y) {
		a.doCraft(a.craftSel)
		d.Invalidate()
		return
	}
	if closeBtn.Contains(ev.X, ev.Y) {
		a.toggleCraft(d)
	}
}

func (a *BlockWorldApp) drawCraftPanel(c *ui.Canvas, client ui.Rect, f *ui.Font) {
	p := craftPanelRect(client)
	c.FillRect(p, ui.ColFace)
	c.Raised(p)
	tb := ui.Rect{p.X + 2, p.Y + 2, p.W - 4, 20}
	c.TitleBar(tb, ui.ColNavy, ui.ColNavyLight)
	f.TextBold(c, tb.X+6, tb.Y+3, "合成面板", ui.ColWhite)
	near := a.nearTable()
	hint := "附近没有工作台 · 只能合成基础配方"
	hcol := uint32(0xA06000)
	if near {
		hint = "工作台就在附近 · 可合成全部配方"
		hcol = 0x207020
	}
	f.Text(c, p.X+12, p.Y+30, hint, hcol)

	for i, r := range bwRecipes {
		row := craftRowRect(client, i)
		sel := i == a.craftSel
		ok := a.canCraft(i)
		if sel {
			c.FillRect(row, ui.ColNavy)
		} else {
			c.FillRect(row, 0xE8E8E8)
		}
		c.Frame(row, 0xA0A0A0)
		nameCol := uint32(ui.ColText)
		if sel {
			nameCol = ui.ColWhite
		} else if !ok {
			nameCol = 0x909090
		}
		f.TextBold(c, row.X+8, row.Y+10, r.Name, nameCol)
		txtCol := uint32(ui.ColText)
		if sel {
			txtCol = ui.ColWhite
		}
		x := row.X + 78
		for _, in := range r.In {
			drawBWBlock(c, in.Block, x, row.Y+4, uint32(in.Block)*97+13)
			cnt := fmt.Sprintf("×%d", in.Count)
			f.Text(c, x+18, row.Y+8, cnt, txtCol)
			x += 18 + f.Width(cnt) + 10
		}
		f.Text(c, x, row.Y+8, "→", txtCol)
		x += 16
		drawBWBlock(c, r.Out.Block, x, row.Y+4, uint32(r.Out.Block)*97+29)
		cnt := fmt.Sprintf("×%d", r.Out.Count)
		f.Text(c, x+18, row.Y+8, cnt, txtCol)
		x += 18 + f.Width(cnt) + 12
		if r.NeedTable {
			tag, tcol := "需工作台", uint32(0xA06000)
			if near {
				tcol = 0x207020
			}
			if sel {
				tcol = 0xFFE080
			}
			f.Text(c, x, row.Y+8, tag, tcol)
		}
	}

	craftBtn, closeBtn := craftButtons(client)
	ui.Button(c, craftBtn, "合成(C)", false, a.canCraft(a.craftSel))
	ui.Button(c, closeBtn, "关闭(Esc)", false, true)
	f.Text(c, p.X+12, p.Bottom()-30, "↑↓ 选择", 0x606060)
}

// drawBlock3D extrudes a block into a lit cube: the normal textured front face
// plus a bright top face and a dark right face whenever those neighbours are
// air. Tile picking still uses the front face, so interaction is unchanged.
func (a *BlockWorldApp) drawBlock3D(c *ui.Canvas, b bwBlock, px, py int, seed uint32, tx, ty int) {
	drawBWBlock(c, b, px, py, seed)
	const depth = 5
	base := bwBaseColor[b]
	if a.world.At(tx, ty-1) == bwAir {
		top := bwShade(base, 1.3)
		for i := 1; i <= depth; i++ {
			c.HLine(px+i, py-i, bwTile, top)
		}
	}
	if a.world.At(tx+1, ty) == bwAir {
		side := bwShade(base, 0.55)
		for i := 1; i <= depth; i++ {
			c.VLine(px+bwTile+i-1, py-i+1, bwTile, side)
		}
	}
}
