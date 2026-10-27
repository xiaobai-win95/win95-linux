package ui

// Arrow draws a small solid triangle. dir is "up", "down", "left" or "right".
func Arrow(c *Canvas, x, y int, dir string, col uint32) {
	switch dir {
	case "up":
		for i := 0; i < 4; i++ {
			for xx := x - i; xx <= x+i; xx++ {
				c.Set(xx, y+i, col)
			}
		}
	case "down":
		for i := 0; i < 4; i++ {
			for xx := x - (3 - i); xx <= x+(3-i); xx++ {
				c.Set(xx, y+i, col)
			}
		}
	case "left":
		for i := 0; i < 4; i++ {
			for yy := y - (3 - i); yy <= y+(3-i); yy++ {
				c.Set(x+i, yy, col)
			}
		}
	case "right":
		for i := 0; i < 4; i++ {
			for yy := y - i; yy <= y+i; yy++ {
				c.Set(x+i, yy, col)
			}
		}
	}
}

// Checkbox draws a check box with optional check mark, using 13x13 pixel cells.
func Checkbox(c *Canvas, x, y int, checked bool, enabled bool) {
	r := Rect{x, y, 13, 13}
	c.FillRect(r, ColWhite)
	shadow := ColShadow
	if !enabled {
		shadow = ColLight
	}
	c.HLine(r.X, r.Y, r.W, shadow)
	c.VLine(r.X, r.Y, r.H, shadow)
	c.HLine(r.X, r.Bottom()-1, r.W, ColWhite)
	c.VLine(r.Right()-1, r.Y, r.H, ColWhite)
	if checked {
		col := ColDark
		if !enabled {
			col = ColShadow
		}
		pts := [][2]int{{3, 6}, {4, 7}, {5, 8}, {6, 7}, {7, 6}, {8, 5}, {9, 4}}
		for _, p := range pts {
			c.Set(x+p[0], y+p[1], col)
			c.Set(x+p[0], y+p[1]+1, col)
		}
	}
}

// Radio draws a round radio button.
func Radio(c *Canvas, x, y int, selected, enabled bool) {
	cx, cy := x+6, y+6
	for yy := 0; yy < 13; yy++ {
		for xx := 0; xx < 13; xx++ {
			dx, dy := xx-cx, yy-cy
			d := dx*dx + dy*dy
			if d <= 36 {
				col := ColDark
				if !enabled {
					col = ColShadow
				}
				if d > 20 {
					c.Set(x+xx, y+yy, col)
				} else if selected {
					c.Set(x+xx, y+yy, ColNavy)
				} else {
					c.Set(x+xx, y+yy, ColWhite)
				}
			}
		}
	}
}

// ProgressBar draws a classic segmented progress bar. value is 0..100.
func ProgressBar(c *Canvas, r Rect, value int) {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	c.FillRect(r, ColWhite)
	c.Sunken(r)
	inner := r.Inset(3)
	c.FillRect(inner, ColFace)
	blocks := inner.W / 9
	filled := blocks * value / 100
	for i := 0; i < filled; i++ {
		c.FillRect(Rect{inner.X + i*9, inner.Y, 7, inner.H}, ColNavy)
	}
}

// HandCursorHotspot is the hand cursor hotspot offset.
const HandCursorHotspot = 6
