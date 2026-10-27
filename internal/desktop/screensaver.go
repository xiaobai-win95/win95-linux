package desktop

import (
	"math/rand"
	"time"

	"win95linux/internal/ui"
)

const saverIdle = 3 * time.Minute

type saverPoint struct {
	x, y   float64
	vx, vy float64
}

type saverTrail struct {
	pts []saverPoint
	col uint32
}

func (d *Desktop) startSaver(now time.Time) {
	d.saver = true
	d.saverAt = now
	d.saverStep = now
	d.saverTrails = d.saverTrails[:0]
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

func (d *Desktop) stepSaver(now time.Time) {
	dt := now.Sub(d.saverStep).Seconds()
	if dt <= 0 {
		return
	}
	if dt > 0.12 {
		dt = 0.12
	}
	d.saverStep = now
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

func (d *Desktop) stopSaver(now time.Time) {
	d.saver = false
	d.saverTrails = nil
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
