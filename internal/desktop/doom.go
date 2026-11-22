package desktop

import (
	"fmt"
	"math"
	"time"

	"win95linux/internal/ui"
)

const (
	doomRW = 320
	doomRH = 200
)

var doomMapRows = []string{
	"####################",
	"#....#........#....#",
	"#....#........#....#",
	"#....#..T##T..#....#",
	"#....#...#....#....#",
	"#........#.........#",
	"###.#.####.####.#..#",
	"#...#.#......#..#..#",
	"#.###.#.####.#..#..#",
	"#.....#.#..#.#.....#",
	"#.#####.#..#.#####.#",
	"#.#.....#..#.....#.#",
	"#.#.###.#..#####.#.#",
	"#.#...#......#...#.#",
	"#.####.#####S#.###.#",
	"#.....#....#.#.....#",
	"#.###.#.##.#.#####.#",
	"#.#...#.#..#.....#.#",
	"#.#.###.#.######.#.#",
	"####################",
}

type doomKeys struct {
	fwd, back, left, right bool
	turnL, turnR           bool
	until                  time.Time
}

type DoomApp struct {
	Base
	d   *Desktop
	win *Window

	px, py float64
	ang    float64

	grid   []byte
	gw, gh int

	frame []uint32
	depth []float64
	tex   [][]uint32

	keys    doomKeys
	client  ui.Rect
	mouseIn bool

	last       time.Time
	lastRender time.Time
	renders    int
	fps        int
	fpsFrames  int
	fpsAt      time.Time

	showMap bool
	flash   time.Time

	xmap  []int
	xmapW int
}

func NewDoomApp(d *Desktop) *DoomApp {
	a := &DoomApp{d: d, px: 2.5, py: 2.5, ang: 0, fps: 30, showMap: true}
	a.loadGrid()
	a.tex = doomTextures()
	a.frame = make([]uint32, doomRW*doomRH)
	a.depth = make([]float64, doomRW)
	a.render()
	return a
}

func (a *DoomApp) Title() string           { return "DOOM 95 - 光线投射迷宫" }
func (a *DoomApp) Icon() ui.IconKind       { return ui.IcoMaze }
func (a *DoomApp) DefaultSize() (int, int) { return 840, 600 }
func (a *DoomApp) MinSize() (int, int)     { return 480, 340 }
func (a *DoomApp) Resizable() bool         { return true }

func (d *Desktop) openDoom() {
	for _, w := range d.windows {
		if _, ok := w.App.(*DoomApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewDoomApp(d)
	a.win = d.Open(a)
}

func (a *DoomApp) loadGrid() {
	a.gh = len(doomMapRows)
	a.gw = 0
	for _, row := range doomMapRows {
		if len(row) > a.gw {
			a.gw = len(row)
		}
	}
	a.grid = make([]byte, a.gw*a.gh)
	for y, row := range doomMapRows {
		for x := 0; x < a.gw; x++ {
			ch := byte('#')
			if x < len(row) {
				ch = row[x]
			}
			id := byte(0)
			switch ch {
			case '#':
				id = 1
			case '=':
				id = 2
			case 'T':
				id = 3
			case 'S':
				id = 4
			case 'D':
				id = 5
			}
			a.grid[y*a.gw+x] = id
		}
	}
}

func (a *DoomApp) cell(x, y int) byte {
	if x < 0 || y < 0 || x >= a.gw || y >= a.gh {
		return 1
	}
	return a.grid[y*a.gw+x]
}

func (a *DoomApp) blocked(x, y float64) bool {
	const r = 0.22
	for _, p := range [4][2]float64{{x - r, y - r}, {x + r, y - r}, {x - r, y + r}, {x + r, y + r}} {
		if a.cell(int(p[0]), int(p[1])) != 0 {
			return true
		}
	}
	return false
}

func (a *DoomApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	a.keys.until = time.Now().Add(280 * time.Millisecond)
	switch ev.Name {
	case "W":
		a.keys.fwd = true
	case "S":
		a.keys.back = true
	case "A":
		a.keys.left = true
	case "D":
		a.keys.right = true
	case "Left":
		a.keys.turnL = true
	case "Right":
		a.keys.turnR = true
	case "M", "Tab":
		a.showMap = !a.showMap
		d.Invalidate()
	case "Esc":
		d.CloseWindow(w)
	case "F1":
		d.MessageBox("DOOM 95", "纯 Go 光线投射迷宫，无 OpenGL。\n\nW/S 前进后退，A/D 平移，\n方向键或鼠标左右转向，\n左键开火，M 开关小地图。", IconInfo, []string{"确定"}, nil)
	default:
		return false
	}
	return true
}

func (a *DoomApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.client = client
	switch ev.Kind {
	case MouseDown:
		if ev.Button == 1 {
			a.flash = time.Now()
			d.Invalidate()
		}
	case MouseMove:
		a.mouseIn = client.Contains(ev.X, ev.Y)
	}
}

func (a *DoomApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	a.client = client
}

func (a *DoomApp) Animate(d *Desktop, w *Window, now time.Time) {
	if a.last.IsZero() {
		a.last = now
	}
	dt := now.Sub(a.last).Seconds()
	if dt > 0.2 {
		dt = 0.2
	}
	if now.After(a.keys.until) {
		a.keys = doomKeys{}
	}
	turn := 0.0
	if a.keys.turnL {
		turn -= 2.1
	}
	if a.keys.turnR {
		turn += 2.1
	}
	if a.client.W > 0 && d.MouseX >= a.client.X && d.MouseX < a.client.Right() &&
		d.MouseY >= a.client.Y && d.MouseY < a.client.Bottom() {
		frac := float64(d.MouseX-a.client.X)/float64(a.client.W)*2 - 1
		if frac > 0.06 || frac < -0.06 {
			turn += frac * 2.4
		}
	}
	a.ang += turn * dt

	dirX, dirY := math.Cos(a.ang), math.Sin(a.ang)
	move := 0.0
	if a.keys.fwd {
		move += 1
	}
	if a.keys.back {
		move -= 1
	}
	strafe := 0.0
	if a.keys.right {
		strafe += 1
	}
	if a.keys.left {
		strafe -= 1
	}
	if move != 0 || strafe != 0 {
		spd := 3.1 * dt
		nx := a.px + (dirX*move-dirY*strafe)*spd
		ny := a.py + (dirY*move+dirX*strafe)*spd
		if !a.blocked(nx, a.py) {
			a.px = nx
		}
		if !a.blocked(a.px, ny) {
			a.py = ny
		}
	}
	a.last = now
	if now.Sub(a.lastRender) < 33*time.Millisecond {
		return
	}
	a.lastRender = now
	a.render()
	a.renders++
	a.fpsFrames++
	if now.Sub(a.fpsAt) >= time.Second {
		a.fps = a.fpsFrames
		a.fpsFrames = 0
		a.fpsAt = now
	}
	d.Invalidate()
}

func (a *DoomApp) render() {
	if a.frame == nil {
		a.frame = make([]uint32, doomRW*doomRH)
		a.depth = make([]float64, doomRW)
	}
	dirX, dirY := math.Cos(a.ang), math.Sin(a.ang)
	planeX, planeY := -dirY*0.66, dirX*0.66

	// Floor and ceiling casting, one mirrored pair of rows at a time.
	half := doomRH / 2
	for y := half; y < doomRH; y++ {
		p := float64(y-half) + 0.5
		rowDist := 0.5 * float64(doomRH) / p
		stepX := rowDist * 2 * planeX / doomRW
		stepY := rowDist * 2 * planeY / doomRW
		fx := a.px + rowDist*(dirX-planeX)
		fy := a.py + rowDist*(dirY-planeY)
		sh := int(240 / (1 + rowDist*0.20))
		if sh > 250 {
			sh = 250
		}
		if sh < 28 {
			sh = 28
		}
		frow := a.frame[y*doomRW:]
		crow := a.frame[(doomRH-1-y)*doomRW:]
		floorTex := a.tex[5]
		ceilTex := a.tex[6]
		for x := 0; x < doomRW; x++ {
			tx := int(fx*64) & 63
			ty := int(fy*64) & 63
			frow[x] = doomShade(floorTex[ty*64+tx], sh)
			crow[x] = doomShade(ceilTex[ty*64+tx], sh)
			fx += stepX
			fy += stepY
		}
	}

	for x := 0; x < doomRW; x++ {
		camX := 2*float64(x)/float64(doomRW) - 1
		rayX := dirX + planeX*camX
		rayY := dirY + planeY*camX
		mapX, mapY := int(a.px), int(a.py)
		deltaX := math.Max(math.Abs(1/rayX), 1e-6)
		deltaY := math.Max(math.Abs(1/rayY), 1e-6)
		stepX, stepY := 1, 1
		sideX, sideY := deltaX, deltaY
		if rayX < 0 {
			stepX = -1
			sideX = (a.px - float64(mapX)) * deltaX
		} else {
			sideX = (float64(mapX) + 1 - a.px) * deltaX
		}
		if rayY < 0 {
			stepY = -1
			sideY = (a.py - float64(mapY)) * deltaY
		} else {
			sideY = (float64(mapY) + 1 - a.py) * deltaY
		}
		side := 0
		var cell byte
		for i := 0; i < 128; i++ {
			if sideX < sideY {
				sideX += deltaX
				mapX += stepX
				side = 0
			} else {
				sideY += deltaY
				mapY += stepY
				side = 1
			}
			cell = a.cell(mapX, mapY)
			if cell != 0 {
				break
			}
		}
		perp := 24.0
		if side == 0 {
			perp = (float64(mapX) - a.px + float64(1-stepX)/2) / rayX
		} else {
			perp = (float64(mapY) - a.py + float64(1-stepY)/2) / rayY
		}
		if perp < 0.05 {
			perp = 0.05
		}
		a.depth[x] = perp

		lineH := int(float64(doomRH) / perp)
		top := doomRH/2 - lineH/2
		start := top
		if start < 0 {
			start = 0
		}
		end := top + lineH
		if end > doomRH {
			end = doomRH
		}
		wallX := 0.0
		if side == 0 {
			wallX = a.py + perp*rayY
		} else {
			wallX = a.px + perp*rayX
		}
		wallX -= math.Floor(wallX)
		texX := int(wallX * 64)
		if (side == 0 && rayX > 0) || (side == 1 && rayY < 0) {
			texX = 63 - texX
		}
		if cell == 0 {
			cell = 1
		}
		tex := a.tex[int(cell)-1]
		shade := int(250 / (1 + perp*0.16))
		if side == 1 {
			shade = shade * 3 / 4
		}
		if shade > 250 {
			shade = 250
		}
		if shade < 28 {
			shade = 28
		}
		texPos := float64(start-top) * 64 / float64(lineH)
		for y := start; y < end; y++ {
			ty := int(texPos) & 63
			texPos += 64 / float64(lineH)
			a.frame[y*doomRW+x] = doomShade(tex[ty*64+texX], shade)
		}
	}
}

func doomShade(col uint32, k int) uint32 {
	if k > 256 {
		k = 256
	}
	if k < 0 {
		k = 0
	}
	r := int(col>>16&0xFF) * k >> 8
	g := int(col>>8&0xFF) * k >> 8
	b := int(col&0xFF) * k >> 8
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

func doomHash(x, y, seed int) uint32 {
	h := uint32(x*73856093) ^ uint32(y*19349663) ^ uint32(seed*83492791)
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return h
}

func doomTextures() [][]uint32 {
	mk := func(fn func(x, y int) uint32) []uint32 {
		t := make([]uint32, 64*64)
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				t[y*64+x] = fn(x, y)
			}
		}
		return t
	}
	brick := mk(func(x, y int) uint32 {
		row := y / 8
		off := 0
		if row%2 == 1 {
			off = 8
		}
		bx := (x + off) % 16
		if y%8 == 0 || bx == 0 || y%8 == 7 {
			return 0x2A1A16
		}
		n := int(doomHash(x/2, y/2, 1)%36) - 18
		return doomShift(0x8A3A2A, n)
	})
	stone := mk(func(x, y int) uint32 {
		if x%16 == 0 || y%16 == 0 {
			return 0x3A3A38
		}
		n := int(doomHash(x/3, y/3, 2)%30) - 15
		return doomShift(0x767468, n)
	})
	tech := mk(func(x, y int) uint32 {
		if x%16 == 0 || y%16 == 0 {
			return 0x0E1C2A
		}
		if y%16 == 8 && x%4 < 2 {
			return 0x37C4DA
		}
		if x%16 == 4 && y%16 == 4 {
			return 0x9AD8E8
		}
		if x%16 == 12 && y%16 == 12 {
			return 0x6A7A88
		}
		n := int(doomHash(x/2, y, 3)%16) - 8
		return doomShift(0x1B3149, n)
	})
	slime := mk(func(x, y int) uint32 {
		h := doomHash(x/4, y/4, 4) % 100
		switch {
		case h < 18:
			return 0x6FAE3E
		case h < 40:
			return 0x4C8A30
		case h < 70:
			return 0x2F6424
		default:
			return 0x1E461C
		}
	})
	door := mk(func(x, y int) uint32 {
		if x%32 == 0 || y%32 == 0 || x%32 == 31 || y%32 == 31 {
			return 0x30343C
		}
		if (x+y)%16 < 5 {
			return 0xD8B520
		}
		n := int(doomHash(x, y/2, 5)%12) - 6
		return doomShift(0x4A4E58, n)
	})
	floor := mk(func(x, y int) uint32 {
		if x%8 == 0 || y%8 == 0 {
			return 0x55555A
		}
		if x%8 == 4 && y%8 == 4 {
			return 0x6A6A70
		}
		n := int(doomHash(x, y, 6)%18) - 9
		return doomShift(0x3A3A3E, n)
	})
	ceiling := mk(func(x, y int) uint32 {
		if x%16 == 0 || y%16 == 0 {
			return 0x1A2434
		}
		h := doomHash(x, y, 7)
		if h%997 < 5 {
			return 0xC8D8FF
		}
		n := int(doomHash(x/2, y/2, 8)%10) - 5
		return doomShift(0x101A28, n)
	})
	return [][]uint32{brick, stone, tech, slime, door, floor, ceiling}
}

func doomShift(col uint32, d int) uint32 {
	r := int(col>>16&0xFF) + d
	g := int(col>>8&0xFF) + d
	b := int(col&0xFF) + d
	clamp := func(v int) uint32 {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint32(v)
	}
	return clamp(r)<<16 | clamp(g)<<8 | clamp(b)
}

func (a *DoomApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, 0x000000)
	if len(a.frame) == 0 {
		a.render()
	}
	scale := math.Min(float64(r.W)/doomRW, float64(r.H)/doomRH)
	if scale <= 0 {
		return
	}
	vw := int(float64(doomRW) * scale)
	vh := int(float64(doomRH) * scale)
	if vw < 1 || vh < 1 {
		return
	}
	vx := r.X + (r.W-vw)/2
	vy := r.Y + (r.H-vh)/2
	if a.xmapW != vw {
		a.xmap = make([]int, vw)
		for x := 0; x < vw; x++ {
			a.xmap[x] = x * doomRW / vw
		}
		a.xmapW = vw
	}
	for y := 0; y < vh; y++ {
		src := a.frame[(y*doomRH/vh)*doomRW:]
		dst := c.Pix[(vy+y)*c.Stride+vx:]
		for x := 0; x < vw; x++ {
			dst[x] = src[a.xmap[x]]
		}
	}

	f := ui.DefaultFont()
	hud := ui.Rect{vx + 6, vy + 6, 172, 40}
	c.FillRect(hud, 0x000000)
	c.Frame(hud, 0x8A8A00)
	f.Text(c, hud.X+6, hud.Y+4, "DOOM 95", 0xFFFF00)
	f.Text(c, hud.X+6, hud.Y+20, fmt.Sprintf("FPS %d   X %.1f Y %.1f", a.fps, a.px, a.py), 0xC0C000)

	hint := "WASD 移动 · 鼠标移向画面左右转向 · 左键开火 · M 地图 · Esc 关闭"
	bw := f.Width(hint) + 12
	if bw > r.W-8 {
		bw = r.W - 8
	}
	bar := ui.Rect{r.X + (r.W-bw)/2, r.Bottom() - 24, bw, 20}
	c.FillRect(bar, 0x000000)
	f.TextClip(c, bar.X+6, bar.Y+3, bar.W-12, hint, 0xC8C8A0)

	// Weapon overlay, drawn above the hint bar.
	gx := vx + vw/2
	gy := vy + vh - 28
	c.FillRect(ui.Rect{gx - 5, gy - 58, 10, 58}, 0x2A2A2E)
	c.FillRect(ui.Rect{gx - 3, gy - 58, 2, 58}, 0x6A6A72)
	c.FillRect(ui.Rect{gx - 16, gy - 18, 32, 18}, 0x4A3A2A)
	c.FillRect(ui.Rect{gx - 13, gy - 22, 8, 10}, 0x8B5A2B)
	c.FillRect(ui.Rect{gx + 5, gy - 22, 8, 10}, 0x8B5A2B)
	if time.Since(a.flash) < 90*time.Millisecond {
		c.FillRect(ui.Rect{gx - 11, gy - 82, 22, 22}, 0xFFFF40)
		c.FillRect(ui.Rect{gx - 5, gy - 76, 10, 10}, 0xFFFFFF)
	}

	if a.showMap {
		ms := ui.Rect{r.Right() - 118, r.Y + 8, a.gw*5 + 6, a.gh*5 + 6}
		if ms.Bottom() < r.Bottom()-30 && ms.X > hud.Right() {
			c.FillRect(ms, 0x000000)
			c.Frame(ms, 0x8A8A00)
			for y := 0; y < a.gh; y++ {
				for x := 0; x < a.gw; x++ {
					cell := a.cell(x, y)
					if cell == 0 {
						continue
					}
					col := uint32(0x8A3A2A)
					switch cell {
					case 2:
						col = 0x767468
					case 3:
						col = 0x37C4DA
					case 4:
						col = 0x4C8A30
					case 5:
						col = 0xD8B520
					}
					c.FillRect(ui.Rect{ms.X + 3 + x*5, ms.Y + 3 + y*5, 5, 5}, col)
				}
			}
			px := ms.X + 3 + int(a.px*5)
			py := ms.Y + 3 + int(a.py*5)
			c.FillRect(ui.Rect{px - 1, py - 1, 3, 3}, 0xFFFFFF)
			for i := 0; i < 8; i++ {
				c.Set(px+int(math.Cos(a.ang)*float64(i)), py+int(math.Sin(a.ang)*float64(i)), 0xFFFF00)
			}
		}
	}
}
