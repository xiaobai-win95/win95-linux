package desktop

import (
	"win95linux/internal/ui"
)

// VScroll is a classic vertical scrollbar model shared by the applications.
type VScroll struct {
	Total   int
	Visible int
	Pos     int
	drag    bool
	dragOff int
	pressed int // 0 none, 1 up, 2 down, 3 track
}

func (s *VScroll) Max() int {
	m := s.Total - s.Visible
	if m < 0 {
		return 0
	}
	return m
}

func (s *VScroll) Clamp() {
	if s.Pos < 0 {
		s.Pos = 0
	}
	if m := s.Max(); s.Pos > m {
		s.Pos = m
	}
	if s.Visible < 1 {
		s.Visible = 1
	}
}

func (s *VScroll) ScrollBy(n int) {
	s.Pos += n
	s.Clamp()
}

func (s *VScroll) Draw(c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	c.HLine(r.X, r.Y, r.W, ui.ColLight)
	c.VLine(r.X, r.Y, r.H, ui.ColLight)
	c.HLine(r.X, r.Bottom()-1, r.W, ui.ColShadow)
	c.VLine(r.Right()-1, r.Y, r.H, ui.ColShadow)

	up := ui.Rect{r.X + 1, r.Y + 1, r.W - 2, 16}
	down := ui.Rect{r.X + 1, r.Bottom() - 17, r.W - 2, 16}
	ui.Button(c, up, "", s.pressed == 1, s.Max() > 0)
	ui.Arrow(c, up.X+up.W/2-2, up.Y+7, "up", ui.ColDark)
	ui.Button(c, down, "", s.pressed == 2, s.Max() > 0)
	ui.Arrow(c, down.X+down.W/2-2, down.Y+7, "down", ui.ColDark)

	trackY := up.Bottom() + 1
	trackH := down.Y - trackY - 1
	if trackH < 8 {
		return
	}
	thumbH := trackH
	if s.Total > s.Visible && s.Total > 0 {
		thumbH = trackH * s.Visible / s.Total
		if thumbH < 16 {
			thumbH = 16
		}
		if thumbH > trackH {
			thumbH = trackH
		}
	}
	thumbY := trackY
	if m := s.Max(); m > 0 {
		thumbY += (trackH - thumbH) * s.Pos / m
	}
	thumb := ui.Rect{r.X + 1, thumbY, r.W - 2, thumbH}
	if s.Max() <= 0 {
		c.FillRect(thumb, ui.ColFace)
		c.HLine(thumb.X, thumb.Y, thumb.W, ui.ColShadow)
		for yy := thumb.Y + 2; yy < thumb.Bottom(); yy += 3 {
			for xx := thumb.X + 2; xx < thumb.Right()-1; xx += 3 {
				c.Set(xx, yy, ui.ColLight)
			}
		}
		return
	}
	ui.Button(c, thumb, "", s.drag, true)
	for yy := thumb.Y + 3; yy < thumb.Bottom()-2; yy += 3 {
		c.HLine(thumb.X+3, yy, thumb.W-6, ui.ColDark)
		c.HLine(thumb.X+3, yy+1, thumb.W-6, ui.ColWhite)
	}
}

func (s *VScroll) thumbRect(r ui.Rect) (ui.Rect, ui.Rect, ui.Rect, ui.Rect) {
	up := ui.Rect{r.X + 1, r.Y + 1, r.W - 2, 16}
	down := ui.Rect{r.X + 1, r.Bottom() - 17, r.W - 2, 16}
	trackY := up.Bottom() + 1
	trackH := down.Y - trackY - 1
	thumbH := trackH
	if s.Total > s.Visible && s.Total > 0 {
		thumbH = trackH * s.Visible / s.Total
		if thumbH < 16 {
			thumbH = 16
		}
		if thumbH > trackH {
			thumbH = trackH
		}
	}
	thumbY := trackY
	if m := s.Max(); m > 0 {
		thumbY += (trackH - thumbH) * s.Pos / m
	}
	return up, down, ui.Rect{r.X + 1, trackY, r.W - 2, trackH}, ui.Rect{r.X + 1, thumbY, r.W - 2, thumbH}
}

// Mouse handles a mouse event local to the scrollbar. It reports whether the
// position changed and whether the event was consumed.
func (s *VScroll) Mouse(ev MouseEvent, r ui.Rect) (bool, bool) {
	s.Clamp()
	up, down, track, thumb := s.thumbRect(r)
	if s.Max() <= 0 {
		return false, r.Contains(ev.X, ev.Y)
	}
	switch ev.Kind {
	case MouseDown:
		switch {
		case up.Contains(ev.X, ev.Y):
			s.pressed = 1
			s.ScrollBy(-1)
			return true, true
		case down.Contains(ev.X, ev.Y):
			s.pressed = 2
			s.ScrollBy(1)
			return true, true
		case thumb.Contains(ev.X, ev.Y):
			s.drag = true
			s.dragOff = ev.Y - thumb.Y
			return false, true
		case track.Contains(ev.X, ev.Y):
			page := s.Visible
			if page < 1 {
				page = 1
			}
			if ev.Y < thumb.Y {
				s.ScrollBy(-page)
			} else {
				s.ScrollBy(page)
			}
			return true, true
		}
	case MouseUp:
		was := s.drag || s.pressed != 0
		s.drag = false
		s.pressed = 0
		return false, was
	case MouseMove:
		if s.drag {
			_, _, track, _ := s.thumbRect(r)
			trackH := track.H
			thumbH := thumb.H
			avail := trackH - thumbH
			if avail <= 0 {
				return false, true
			}
			y := ev.Y - s.dragOff - track.Y
			pos := y * s.Max() / avail
			if pos != s.Pos {
				s.Pos = pos
				s.Clamp()
				return true, true
			}
			return false, true
		}
		return false, false
	case MouseWheel:
		before := s.Pos
		s.ScrollBy(-ev.Wheel)
		return s.Pos != before, r.Contains(ev.X, ev.Y)
	}
	return false, r.Contains(ev.X, ev.Y)
}
