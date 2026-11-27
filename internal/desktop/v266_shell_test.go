package desktop

import (
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestWindowResizeHitAreas(t *testing.T) {
	w := &Window{X: 100, Y: 80, W: 400, H: 300, Resizable: true}
	cases := []struct {
		x, y int
		want resizeDirection
	}{
		{100, 80, resizeTopLeft},
		{499, 80, resizeTopRight},
		{100, 379, resizeBottomLeft},
		{499, 379, resizeBottomRight},
		{101, 200, resizeLeft},
		{498, 200, resizeRight},
		{300, 81, resizeTop},
		{300, 378, resizeBottom},
		{300, 200, resizeNone},
	}
	for _, tc := range cases {
		if got := w.resizeEdgeAt(tc.x, tc.y); got != tc.want {
			t.Errorf("resizeEdgeAt(%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	w.Maximized = true
	if got := w.resizeEdgeAt(100, 80); got != resizeNone {
		t.Fatalf("maximized window exposed resize edge: %v", got)
	}
}

func TestResizeWindowBounds(t *testing.T) {
	start := ui.Rect{X: 100, Y: 80, W: 400, H: 300}
	cases := []struct {
		dir    resizeDirection
		dx, dy int
		want   ui.Rect
	}{
		{resizeRight, 50, 0, ui.Rect{X: 100, Y: 80, W: 450, H: 300}},
		{resizeBottom, 0, 50, ui.Rect{X: 100, Y: 80, W: 400, H: 350}},
		{resizeLeft, 50, 0, ui.Rect{X: 150, Y: 80, W: 350, H: 300}},
		{resizeTop, 0, 50, ui.Rect{X: 100, Y: 130, W: 400, H: 250}},
		{resizeBottomRight, 50, 50, ui.Rect{X: 100, Y: 80, W: 450, H: 350}},
		{resizeTopLeft, 50, 50, ui.Rect{X: 150, Y: 130, W: 350, H: 250}},
		{resizeRight, 900, 0, ui.Rect{X: 100, Y: 80, W: 900, H: 300}},
		{resizeBottom, 0, 900, ui.Rect{X: 100, Y: 80, W: 400, H: 650}},
		{resizeLeft, -500, 0, ui.Rect{X: 0, Y: 80, W: 500, H: 300}},
		{resizeRight, -900, 0, ui.Rect{X: 100, Y: 80, W: 220, H: 300}},
	}
	for _, tc := range cases {
		got := resizeWindowBounds(start, tc.dir, tc.dx, tc.dy, 220, 140, 1000, 730)
		if got != tc.want {
			t.Errorf("resize %v (%d,%d) = %+v, want %+v", tc.dir, tc.dx, tc.dy, got, tc.want)
		}
	}
}

type resizeTestApp struct {
	Base
	resized int
	last    ui.Rect
}

func (a *resizeTestApp) Title() string                                { return "Resize test" }
func (a *resizeTestApp) Icon() ui.IconKind                            { return ui.IcoFile }
func (a *resizeTestApp) Paint(*Desktop, *Window, *ui.Canvas, ui.Rect) {}
func (a *resizeTestApp) Resized(_ *Desktop, _ *Window, client ui.Rect) {
	a.resized++
	a.last = client
}

func TestWindowResizeDragUpdatesClient(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	app := &resizeTestApp{}
	w := d.Open(app)
	w.X, w.Y, w.W, w.H = 100, 80, 400, 300
	d.Mouse(MouseEvent{Kind: MouseMove, X: w.X + w.W - 1, Y: w.Y + 100})
	if d.resizeCursor != "ew" {
		d.CloseWindow(w)
		t.Fatalf("right edge cursor = %q", d.resizeCursor)
	}
	d.Mouse(MouseEvent{Kind: MouseDown, X: w.X + w.W - 1, Y: w.Y + 100, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseMove, X: w.X + w.W + 60, Y: w.Y + 100})
	d.Mouse(MouseEvent{Kind: MouseUp, X: w.X + w.W + 60, Y: w.Y + 100, Button: 1})
	if w.W != 461 || app.resized == 0 || app.last.W != w.ClientRect().W {
		d.CloseWindow(w)
		t.Fatalf("resize produced window=%+v client=%+v callbacks=%d", w.Outer(), app.last, app.resized)
	}
	d.CloseWindow(w)
}

func TestProgramsMenuScrollsWithinScreen(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.startOpen = true
	d.setStartSub("prog")
	entries := d.menuEntries()
	idx := -1
	for i, e := range entries {
		if e.id == "prog" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("programs menu missing")
	}
	e := entries[idx]
	sr := d.startSubRect(d.startItemRects()[idx], e)
	if sr.Bottom() > d.taskbarTop() || sr.Y < 0 {
		t.Fatalf("programs menu outside workspace: %+v", sr)
	}
	if d.startSubScrollMax(sr, e) <= 0 {
		t.Fatal("long programs menu did not overflow")
	}
	d.Mouse(MouseEvent{Kind: MouseWheel, X: sr.X + 40, Y: sr.Y + 40, Wheel: -1})
	if d.startSubScroll <= 0 {
		t.Fatal("wheel did not scroll programs menu")
	}
	d.startSubScroll = d.startSubScrollMax(sr, e)
	view := d.startSubViewport(sr, e)
	if _, id := d.startSubAt(view.X+10, view.Bottom()-20); id != "prank" {
		t.Fatalf("last programs entry not reachable, got %q", id)
	}
	d.Draw(ui.NewCanvas(d.W, d.H))
}

func TestSmallDesktopStartMenuScrolls(t *testing.T) {
	d := New(320, 300, ModeDesktop, Host{})
	d.startOpen = true
	if d.startScrollMax() <= 0 {
		t.Fatal("small desktop start menu did not overflow")
	}
	r := d.startMenuRect()
	d.Mouse(MouseEvent{Kind: MouseWheel, X: r.X + 50, Y: r.Y + 30, Wheel: -1})
	if d.startScroll <= 0 {
		t.Fatal("start menu wheel did not scroll")
	}
	d.Draw(ui.NewCanvas(d.W, d.H))
}

func TestX11PointerMappingTracksScaledPicture(t *testing.T) {
	a := &X11ProxyApp{sess: &x11Session{width: 1000, height: 700}, screenR: ui.Rect{X: 100, Y: 80, W: 500, H: 350}}
	x, y, ok := a.toX11(350, 255)
	if !ok || x != 500 || y != 350 {
		t.Fatalf("scaled X11 pointer = %d,%d,%v", x, y, ok)
	}
	if _, _, ok := a.toX11(99, 255); ok {
		t.Fatal("pointer outside scaled picture reached X11")
	}
}

func TestHighContrastInputAndNotificationPixels(t *testing.T) {
	for _, p := range ui.Palettes {
		ui.ApplyPalette(p)
		d := &Desktop{W: 800, H: 600, lang: "zh-CN"}
		a := &InputBoxApp{d: d, title: "Test", prompt: "Prompt", value: []rune("ABC"), caret: 3}
		c := ui.NewCanvas(440, 176)
		a.Paint(d, nil, c, ui.Rect{W: 440, H: 176})
		if !countRectColors(c, a.fieldR.Inset(4), ui.ColField, ui.ColText) {
			t.Errorf("%s input field lost foreground/background pixels", p.Name)
		}
		d.notices = []notice{{title: "Title", text: "Message", at: time.Now()}}
		n := ui.NewCanvas(800, 600)
		d.drawNotifications(n)
		bg, fg := ui.ColYellow, ui.TextOn(ui.ColYellow)
		if !countRectColors(n, d.noticeRect, bg, fg) {
			t.Errorf("%s notification lost foreground/background pixels", p.Name)
		}
	}
	ui.ApplyPalette(ui.Palettes[0])
}

func countRectColors(c *ui.Canvas, r ui.Rect, a, b uint32) bool {
	xa, xb := 0, 0
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X; x < r.Right(); x++ {
			switch c.Pix[y*c.Stride+x] {
			case a:
				xa++
			case b:
				xb++
			}
		}
	}
	return xa > 0 && xb > 0
}
