package desktop

import (
	"math"

	"win95linux/internal/ui"
)

var backgroundStyles = []string{
	"经典青色",
	"纯青色",
	"海洋蓝",
	"云彩",
	"午夜星空",
	"石板灰",
}

func (d *Desktop) drawDesktopBackground(c *ui.Canvas) {
	drawBackgroundRect(c, ui.Rect{0, 0, c.W, c.H}, d.background)
}

func drawBackgroundRect(c *ui.Canvas, r ui.Rect, style int) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	switch style {
	case 1:
		c.FillRect(r, ui.ColDesktop)
	case 2:
		paintDither(c, r, 0x000080, 0x000060)
	case 3:
		drawCloudWallpaper(c, r)
	case 4:
		drawNightWallpaper(c, r)
	case 5:
		paintDither(c, r, 0x808080, 0x787878)
	default:
		paintDither(c, r, ui.ColDesktop, 0x007F7F)
	}
}

func paintDither(c *ui.Canvas, r ui.Rect, base, dot uint32) {
	c.FillRect(r, base)
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X + ((y - r.Y) & 1); x < r.Right(); x += 2 {
			c.Set(x, y, dot)
		}
	}
}

func bgSeed(n int) uint32 {
	x := uint32(n)*2654435761 + 0x9E3779B9
	x ^= x >> 16
	x *= 0x7FEB352D
	x ^= x >> 15
	return x
}

func mixColor(a, b uint32, t float64) uint32 {
	ar, ag, ab := (a>>16)&0xff, (a>>8)&0xff, a&0xff
	br, bg, bb := (b>>16)&0xff, (b>>8)&0xff, b&0xff
	r := uint32(float64(ar) + (float64(br)-float64(ar))*t)
	g := uint32(float64(ag) + (float64(bg)-float64(ag))*t)
	bl := uint32(float64(ab) + (float64(bb)-float64(ab))*t)
	return r<<16 | g<<8 | bl
}

func fillEllipse(c *ui.Canvas, r ui.Rect, cx, cy, rx, ry int, col uint32) {
	if rx <= 0 || ry <= 0 {
		return
	}
	for dy := -ry; dy <= ry; dy++ {
		t := float64(dy) / float64(ry)
		if t*t >= 1 {
			continue
		}
		half := int(float64(rx) * math.Sqrt(1-t*t))
		y := cy + dy
		if y < r.Y || y >= r.Bottom() {
			continue
		}
		x0, x1 := cx-half, cx+half
		if x0 < r.X {
			x0 = r.X
		}
		if x1 >= r.Right() {
			x1 = r.Right() - 1
		}
		for x := x0; x <= x1; x++ {
			c.Set(x, y, col)
		}
	}
}

func drawCloud(c *ui.Canvas, r ui.Rect, cx, cy, size int, col uint32) {
	fillEllipse(c, r, cx, cy, size, size/2, col)
	fillEllipse(c, r, cx-size*3/4, cy+size/8, size*3/5, size/3, col)
	fillEllipse(c, r, cx+size*3/4, cy+size/7, size*3/5, size/3, col)
	fillEllipse(c, r, cx+size/4, cy-size/3, size/2, size/3, col)
	fillEllipse(c, r, cx-size/3, cy-size/4, size/2, size/3, col)
}

func drawCloudWallpaper(c *ui.Canvas, r ui.Rect) {
	const top, bottom = 0x2B5B8A, 0x8FB6D6
	for y := 0; y < r.H; y++ {
		t := float64(y) / float64(maxInt(1, r.H-1))
		c.HLine(r.X, r.Y+y, r.W, mixColor(top, bottom, t))
	}
	for i := 0; i < 6; i++ {
		h := bgSeed(i*977 + 31)
		size := r.W/10 + int(h%uint32(maxInt(1, r.W/16)))
		cx := r.X - size/2 + int(bgSeed(i*131+7)%uint32(maxInt(1, r.W+size)))
		cy := r.Y + int(bgSeed(i*613+3)%uint32(maxInt(1, r.H*3/5)))
		drawCloud(c, r, cx, cy+4, size, 0x7C98B8)
		drawCloud(c, r, cx, cy, size, 0xF2F7FC)
		drawCloud(c, r, cx-size/8, cy-size/8, size*3/4, 0xFFFFFF)
	}
}

func drawNightWallpaper(c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, 0x000018)
	for i := 0; i < 260; i++ {
		x := r.X + int(bgSeed(i*2654435761+17)%uint32(maxInt(1, r.W)))
		y := r.Y + int(bgSeed(i*40503+97)%uint32(maxInt(1, r.H)))
		col := uint32(0x707090)
		switch {
		case i%11 == 0:
			col = 0xFFFFFF
		case i%4 == 0:
			col = 0xB0B0D8
		}
		c.Set(x, y, col)
		if i%11 == 0 {
			c.Set(x+1, y, 0x9090B8)
			c.Set(x, y+1, 0x9090B8)
		}
	}
	mx := r.X + r.W*3/4
	my := r.Y + r.H/4
	fillEllipse(c, r, mx, my, r.W/24, r.W/24, 0xE8E8D0)
	fillEllipse(c, r, mx+r.W/48, my-r.W/64, r.W/26, r.W/26, 0x000018)
}
