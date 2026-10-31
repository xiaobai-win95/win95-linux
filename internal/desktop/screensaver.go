package desktop

import (
	"math/rand"
	"time"

	"win95linux/internal/ui"
)

const (
	saverIdleDefault = 3 * time.Minute
	saverMystify     = 1
	saverStarfield   = 2
	saverFlying      = 3
)

type saverPoint struct {
	x, y   float64
	vx, vy float64
}

type saverTrail struct {
	pts []saverPoint
	col uint32
}

type saverStar struct {
	x, y, z float64
}

type saverWindow struct {
	x, y, z float64
}

func (d *Desktop) startSaver(now time.Time) {
	d.saver = true
	d.saverAt = now
	d.saverStep = now
	d.saverTrails = d.saverTrails[:0]
	d.saverStars = d.saverStars[:0]
	d.saverWindows = d.saverWindows[:0]
	switch d.saverStyle {
	case saverStarfield:
		d.initStarfield()
	case saverFlying:
		d.initFlyingWindows()
	default:
		d.initMystify()
	}
}

func (d *Desktop) initMystify() {
	colors := []uint32{0x00FFFF, 0xFF00FF}
	w, h := float64(d.W), float64(d.H)
	for t := 0; t < 2; t++ {
		col := colors[t%len(colors)]
		pts := make([]saverPoint, 6)
		x := 80 + rand.Float64()*(w-160)
		y := 80 + rand.Float64()*(h-160)
		for i := range pts {
			pts[i] = saverPoint{
				x:  x + float64(i)*18,
				y:  y + float64(i)*12,
				vx: (rand.Float64()*2 - 1) * 190,
				vy: (rand.Float64()*2 - 1) * 150,
			}
		}
		d.saverTrails = append(d.saverTrails, saverTrail{pts: pts, col: col})
		// The second trail is a mirrored ghost of the first, like Mystify.
		mirror := make([]saverPoint, len(pts))
		for i, p := range pts {
			mirror[i] = saverPoint{x: w - p.x, y: p.y, vx: -p.vx, vy: p.vy}
		}
		d.saverTrails = append(d.saverTrails, saverTrail{pts: mirror, col: col})
	}
}

func (d *Desktop) initStarfield() {
	d.saverStars = make([]saverStar, 220)
	for i := range d.saverStars {
		d.saverStars[i] = saverStar{
			x: rand.Float64()*2 - 1,
			y: rand.Float64()*2 - 1,
			z: 0.05 + rand.Float64()*0.95,
		}
	}
}

func (d *Desktop) stepSaver(now time.Time) {
	dt := now.Sub(d.saverStep).Seconds()
	if dt <= 0 {
		return
	}
	if dt > 0.12 {
		dt = 0.12
	}
	d.saverStep = now
	switch d.saverStyle {
	case saverStarfield:
		d.stepStarfield(dt)
	case saverFlying:
		d.stepFlyingWindows(dt)
	default:
		d.stepMystify(dt)
	}
}

func (d *Desktop) stepMystify(dt float64) {
	w, h := float64(d.W), float64(d.H)
	for ti := range d.saverTrails {
		for i := range d.saverTrails[ti].pts {
			p := &d.saverTrails[ti].pts[i]
			p.x += p.vx * dt
			p.y += p.vy * dt
			if p.x < 4 {
				p.x = 4
				p.vx = -p.vx
			}
			if p.x > w-4 {
				p.x = w - 4
				p.vx = -p.vx
			}
			if p.y < 4 {
				p.y = 4
				p.vy = -p.vy
			}
			if p.y > h-4 {
				p.y = h - 4
				p.vy = -p.vy
			}
		}
	}
}

func (d *Desktop) stepStarfield(dt float64) {
	for i := range d.saverStars {
		s := &d.saverStars[i]
		s.z -= dt * 0.36
		if s.z <= 0.03 {
			s.x = rand.Float64()*2 - 1
			s.y = rand.Float64()*2 - 1
			s.z = 1
		}
	}
}

func (d *Desktop) stopSaver(now time.Time) {
	d.saver = false
	d.saverTrails = nil
	d.saverStars = nil
	d.saverWindows = nil
	d.lastInput = now
	d.Invalidate()
}

func drawSaverLine(c *ui.Canvas, x0, y0, x1, y1 int, col uint32) {
	dx := x1 - x0
	dy := y1 - y0
	steps := dx
	if steps < 0 {
		steps = -steps
	}
	ay := dy
	if ay < 0 {
		ay = -ay
	}
	if ay > steps {
		steps = ay
	}
	if steps == 0 {
		c.Set(x0, y0, col)
		c.Set(x0+1, y0, col)
		return
	}
	for i := 0; i <= steps; i++ {
		x := x0 + dx*i/steps
		y := y0 + dy*i/steps
		c.Set(x, y, col)
		c.Set(x+1, y, col)
	}
}

func fadeColor(col uint32, f float64) uint32 {
	r := uint32(float64((col>>16)&0xff) * f)
	g := uint32(float64((col>>8)&0xff) * f)
	b := uint32(float64(col&0xff) * f)
	return r<<16 | g<<8 | b
}

func (d *Desktop) drawScreensaver(c *ui.Canvas) {
	c.FillRect(ui.Rect{0, 0, c.W, c.H}, 0x000000)
	switch d.saverStyle {
	case saverStarfield:
		d.drawStarfield(c)
	case saverFlying:
		d.drawFlyingWindows(c)
	default:
		d.drawMystify(c)
	}
}

func (d *Desktop) drawMystify(c *ui.Canvas) {
	for _, tr := range d.saverTrails {
		for i := 0; i+1 < len(tr.pts); i++ {
			a, b := tr.pts[i], tr.pts[i+1]
			f := 1.0 - float64(i)/float64(len(tr.pts)-1)*0.75
			drawSaverLine(c, int(a.x), int(a.y), int(b.x), int(b.y), fadeColor(tr.col, f))
		}
		if len(tr.pts) > 0 {
			head := tr.pts[0]
			c.FillRect(ui.Rect{int(head.x) - 1, int(head.y) - 1, 3, 3}, tr.col)
		}
	}
}

func (d *Desktop) initFlyingWindows() {
	d.saverWindows = make([]saverWindow, 32)
	for i := range d.saverWindows {
		d.spawnFlyingWindow(&d.saverWindows[i], 0.08+rand.Float64()*0.92)
	}
}

func (d *Desktop) spawnFlyingWindow(s *saverWindow, z float64) {
	s.x = rand.Float64()*0.5 - 0.25
	s.y = rand.Float64()*0.5 - 0.25
	s.z = z
}

func (d *Desktop) stepFlyingWindows(dt float64) {
	dim := float64(minInt(d.W, d.H))
	scale := dim * 0.45
	for i := range d.saverWindows {
		s := &d.saverWindows[i]
		s.z -= dt * 0.34
		if s.z < 0.065 {
			d.spawnFlyingWindow(s, 1)
			continue
		}
		cx := float64(d.W)/2 + s.x/s.z*scale
		cy := float64(d.H)/2 + s.y/s.z*scale
		size := dim * 0.028 / s.z
		if size > dim*0.5 {
			size = dim * 0.5
		}
		if cx+size < 0 || cx-size > float64(d.W) || cy+size < 0 || cy-size > float64(d.H) {
			d.spawnFlyingWindow(s, 1)
		}
	}
}

func (d *Desktop) drawFlyingWindows(c *ui.Canvas) {
	dim := float64(minInt(c.W, c.H))
	scale := dim * 0.45
	for _, s := range d.saverWindows {
		if s.z <= 0.065 {
			continue
		}
		cx := float64(c.W)/2 + s.x/s.z*scale
		cy := float64(c.H)/2 + s.y/s.z*scale
		size := int(dim * 0.028 / s.z)
		if size < 12 {
			size = 12
		}
		if size > int(dim*0.5) {
			size = int(dim * 0.5)
		}
		ww := size
		hh := size * 3 / 5
		if hh < 10 {
			hh = 10
		}
		x := int(cx) - ww/2
		y := int(cy) - hh/2
		if x+ww < 0 || x > c.W || y+hh < 0 || y > c.H {
			continue
		}
		drawFlyingWindow(c, x, y, ww, hh)
	}
}

func drawFlyingWindow(c *ui.Canvas, x, y, w, h int) {
	if w < 12 || h < 10 {
		return
	}
	c.FillRect(ui.Rect{x + 3, y + 3, w, h}, 0x303030)
	c.FillRect(ui.Rect{x, y, w, h}, ui.ColFace)
	c.Frame(ui.Rect{x, y, w, h}, ui.ColDark)
	titleH := h / 5
	if titleH < 4 {
		titleH = 4
	}
	title := ui.Rect{x + 1, y + 1, w - 2, titleH}
	c.FillRect(title, ui.ColNavy)
	// The title bar lights up as the window comes towards the viewer.
	if w > 28 {
		c.HLine(title.X, title.Y, title.W, ui.ColNavyLight)
	}
	if w > 42 {
		lineW := w / 3
		if lineW > 28 {
			lineW = 28
		}
		c.FillRect(ui.Rect{title.X + 3, title.Y + titleH/2 - 1, lineW, 1}, 0xD0D0FF)
		btnY := title.Y + titleH/2 - 1
		for i := 0; i < 3; i++ {
			bx := title.Right() - 4 - i*5
			if bx < title.X+2 {
				break
			}
			c.FillRect(ui.Rect{bx, btnY, 3, 3}, ui.ColFace)
			c.FillRect(ui.Rect{bx, btnY, 3, 1}, ui.ColWhite)
		}
	}
	if h > 20 {
		c.HLine(x+1, y+titleH+1, w-2, ui.ColShadow)
		c.FillRect(ui.Rect{x + w/5, y + titleH + h/3, w * 3 / 5, 2}, 0x808080)
	}
}

func (d *Desktop) drawStarfield(c *ui.Canvas) {
	cx, cy := c.W/2, c.H/2
	scale := float64(minInt(c.W, c.H)) * 0.62
	for _, s := range d.saverStars {
		if s.z <= 0.03 {
			continue
		}
		bright := 1 - s.z
		px := cx + int(s.x/s.z*scale)
		py := cy + int(s.y/s.z*scale)
		if px < -80 || px > c.W+80 || py < -80 || py > c.H+80 {
			continue
		}
		col := fadeColor(0xB4C8FF, 0.38+0.62*bright)
		if bright > 0.7 {
			col = fadeColor(0xFFFFFF, 0.75+0.25*bright)
		}
		z2 := s.z + 0.02 + bright*0.04
		px2 := cx + int(s.x/z2*scale)
		py2 := cy + int(s.y/z2*scale)
		drawSaverLine(c, px2, py2, px, py, col)
		if bright > 0.8 {
			c.Set(px+1, py, col)
			c.Set(px, py+1, col)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
