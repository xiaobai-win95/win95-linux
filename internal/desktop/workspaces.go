package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

const workspaceCount = 4

// switchWorkspace moves to a 1-based workspace index (1..workspaceCount).
func (d *Desktop) switchWorkspace(n int) {
	if d.modal != nil || n < 1 || n > workspaceCount {
		return
	}
	d.ws = n - 1
	d.startOpen = false
	d.startSub = ""
	d.startHover = ""
	d.startSel = ""
	d.ctx = nil
	d.selected = -1
	d.dragIcon = -1
	d.capture = nil
	d.active = nil
	for i := len(d.windows) - 1; i >= 0; i-- {
		c := d.windows[i]
		if !c.Minimized && !c.Modal && c.Workspace == d.ws {
			d.active = c
			break
		}
	}
	d.wsToast = fmt.Sprintf("桌面 %d", n)
	d.wsToastUntil = time.Now().Add(900 * time.Millisecond)
	d.Invalidate()
}

const (
	tileLeft = iota
	tileRight
	tileTop
	tileBottom
)

// tileActive snaps the active window to half of the current workspace. The
// previous bounds are remembered so Alt+0 (or dragging the title bar) restores
// the floating size.
func (d *Desktop) tileActive(side int) {
	w := d.active
	if d.modal != nil || w == nil || w.Modal || !w.Resizable || w.Minimized {
		return
	}
	if !w.tiled {
		w.tilePrev = ui.Rect{w.X, w.Y, w.W, w.H}
		w.tiled = true
	}
	const margin = 4
	gap := 4
	area := d.workspace()
	left := area.X + margin
	top := area.Y + margin
	right := area.Right() - margin
	bottom := area.Bottom() - margin
	halfW := (right - left - gap) / 2
	halfH := (bottom - top - gap) / 2
	switch side {
	case tileLeft:
		w.X, w.Y, w.W, w.H = left, top, halfW, bottom-top
	case tileRight:
		w.X, w.Y, w.W, w.H = left+halfW+gap, top, halfW, bottom-top
	case tileTop:
		w.X, w.Y, w.W, w.H = left, top, right-left, halfH
	case tileBottom:
		w.X, w.Y, w.W, w.H = left, top+halfH+gap, right-left, halfH
	}
	if w.W < w.MinW {
		w.W = w.MinW
	}
	if w.H < w.MinH {
		w.H = w.MinH
	}
	w.Maximized = false
	w.App.Resized(d, w, w.ClientRect())
	d.Invalidate()
}

func (d *Desktop) untileActive() {
	w := d.active
	if w == nil || !w.tiled {
		return
	}
	w.X, w.Y, w.W, w.H = w.tilePrev.X, w.tilePrev.Y, w.tilePrev.W, w.tilePrev.H
	w.tiled = false
	w.App.Resized(d, w, w.ClientRect())
	d.Invalidate()
}

// untiledBounds is called when a tiled window starts a drag or resize so it
// becomes floating again at its previous size.
func (d *Desktop) unfloatForDrag(w *Window, gx, gy int) {
	if w == nil || !w.tiled {
		return
	}
	prev := w.tilePrev
	w.W, w.H = prev.W, prev.H
	w.X = gx - prev.W/2
	w.Y = gy - titleH/2
	if w.X < 8 {
		w.X = 8
	}
	if w.Y < 8 {
		w.Y = 8
	}
	w.tiled = false
	w.App.Resized(d, w, w.ClientRect())
}

func (d *Desktop) drawWorkspaceToast(c *ui.Canvas) {
	if d.wsToast == "" {
		return
	}
	f := ui.DefaultFont()
	w := 180
	h := 56
	x := (d.W - w) / 2
	y := d.taskbarTop() - h - 24
	r := ui.Rect{x, y, w, h}
	c.FillRect(ui.Rect{r.X + 3, r.Y + 3, r.W, r.H}, ui.ColShadow)
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	f.TextBold(c, x+(w-f.Width(d.wsToast))/2, y+10, d.wsToast, ui.ColText)
	for i := 0; i < workspaceCount; i++ {
		bx := x + w/2 - workspaceCount*9 + i*18
		br := ui.Rect{bx, y + 34, 12, 8}
		if i == d.ws {
			c.FillRect(br, ui.ColNavy)
		} else {
			c.FillRect(br, ui.ColWhite)
			c.Frame(br, ui.ColShadow)
		}
	}
}
