package desktop

import (
	"time"

	"win95linux/internal/ui"
)

type Mode int

const (
	ModeDesktop Mode = iota
	ModeInstaller
)

type DisplayMode struct {
	Width    int
	Height   int
	Refresh  int
	Rotation int
}

func (m DisplayMode) Normalized() DisplayMode {
	if m.Width < 640 {
		m.Width = 640
	}
	if m.Height < 480 {
		m.Height = 480
	}
	if m.Refresh < 24 || m.Refresh > 240 {
		m.Refresh = 60
	}
	m.Rotation %= 360
	if m.Rotation < 0 {
		m.Rotation += 360
	}
	if m.Rotation%90 != 0 {
		m.Rotation = 0
	}
	return m
}

func (m DisplayMode) LogicalSize() (int, int) {
	m = m.Normalized()
	if m.Rotation == 90 || m.Rotation == 270 {
		return m.Height, m.Width
	}
	return m.Width, m.Height
}

func (m DisplayMode) LogicalPoint(x, y, physicalW, physicalH int) (int, int) {
	switch m.Normalized().Rotation {
	case 90:
		return physicalH - 1 - y, x
	case 180:
		return physicalW - 1 - x, physicalH - 1 - y
	case 270:
		return y, physicalW - 1 - x
	default:
		return x, y
	}
}

func (m DisplayMode) PhysicalPoint(x, y, physicalW, physicalH int) (int, int) {
	switch m.Normalized().Rotation {
	case 90:
		return y, physicalH - 1 - x
	case 180:
		return physicalW - 1 - x, physicalH - 1 - y
	case 270:
		return physicalW - 1 - y, x
	default:
		return x, y
	}
}

// Host connects the UI to machine-level actions such as power management.
type Host struct {
	Reboot             func()
	PowerOff           func()
	Logout             func()
	SetDisplayMode     func(DisplayMode) (DisplayMode, error)
	CurrentDisplayMode func() DisplayMode
	AvailableDisplays  func() []DisplayMode
	// Suspend blocks until the machine wakes from sleep. The desktop has
	// already painted its sleep screen when this is called.
	Suspend func() error
	// Wake asks the host event loop to process queued UI callbacks.
	Wake func()
	// Screenshot saves the currently displayed frame as a PNG and a BMP and
	// returns both paths separated by a newline.
	Screenshot func() (string, error)
	// Visible reports whether the framebuffer is on screen. Off-screen
	// notifications are queued until the desktop is visible again.
	Visible func() bool
	Info    string
}

type MouseKind int

const (
	MouseMove MouseKind = iota
	MouseDown
	MouseUp
	MouseWheel
)

type MouseEvent struct {
	Kind   MouseKind
	X, Y   int
	Button int
	Wheel  int
	Shift  bool
	Ctrl   bool
}

type KeyEvent struct {
	Rune  rune
	Name  string
	Shift bool
	Ctrl  bool
	Alt   bool
}

// App is the content inside a managed window. Mouse coordinates and client
// rectangles use framebuffer (screen) coordinates, matching Paint.
type App interface {
	Title() string
	Icon() ui.IconKind
	Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect)
	Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent)
	Key(d *Desktop, w *Window, ev KeyEvent) bool
	Close()
	Resized(d *Desktop, w *Window, client ui.Rect)
}

// Base supplies no-op behaviour for optional App methods.
type Base struct{}

func (Base) Mouse(*Desktop, *Window, ui.Rect, MouseEvent) {}
func (Base) Key(*Desktop, *Window, KeyEvent) bool         { return false }
func (Base) Close()                                       {}
func (Base) Resized(*Desktop, *Window, ui.Rect)           {}

type Menu struct {
	Label string
	Items []MenuItem
}

type MenuItem struct {
	Label     string
	Accel     string
	Separator bool
	Checked   bool
	Enabled   bool
	Action    func()
}

type MenuProvider interface {
	Menus(d *Desktop, w *Window) []Menu
}

// ClientContextMenu lets an app supply its own right-click menu inside the
// window client area. Coordinates are framebuffer coordinates.
type ClientContextMenu interface {
	ContextMenu(d *Desktop, w *Window, x, y int) []ctxItem
}

// ClientRightClick lets an app consume right-clicks inside its client area
// before the shell offers a context menu (used by the X11 proxy).
type ClientRightClick interface {
	RightClick(d *Desktop, w *Window, x, y int) bool
}

// Animator is implemented by apps that need periodic updates (for example
// JavaScript timers in the browser or game physics).
type Animator interface {
	Animate(d *Desktop, w *Window, now time.Time)
}

type CloseRequester interface {
	RequestClose(d *Desktop, w *Window) bool
}

type Window struct {
	App        App
	X, Y, W, H int
	MinW       int
	MinH       int
	Resizable  bool
	Modal      bool
	Minimized  bool
	Maximized  bool
	Workspace  int
	tiled      bool
	tilePrev   ui.Rect
	restore    ui.Rect

	menus      []Menu
	menuOpen   int
	menuHover  int
	pressedBtn string
	drag       bool
	dragDX     int
	dragDY     int
	resizing   bool
	resizeDir  resizeDirection
	resizeX    int
	resizeY    int
	titleClick time.Time
	created    time.Time
	prevActive *Window
}

const (
	frame      = 3
	titleH     = 24
	menuBarH   = 22
	taskbarH   = 34
	btnW       = 20
	btnH       = 18
	startBtnW  = 86
	startMenuW = 248
	clockW     = 82
	sysTrayW   = 122
)

func (w *Window) TitleRect() ui.Rect {
	return ui.Rect{X: w.X + frame, Y: w.Y + frame, W: w.W - 2*frame, H: titleH}
}

func (w *Window) MenuBarRect() ui.Rect {
	if len(w.menus) == 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: w.X + frame, Y: w.Y + frame + titleH, W: w.W - 2*frame, H: menuBarH}
}

func (w *Window) ClientRect() ui.Rect {
	top := w.Y + frame + titleH
	h := w.H - 2*frame - titleH
	if len(w.menus) > 0 {
		top += menuBarH
		h -= menuBarH
	}
	return ui.Rect{X: w.X + frame, Y: top, W: w.W - 2*frame, H: h}
}

func (w *Window) Outer() ui.Rect { return ui.Rect{X: w.X, Y: w.Y, W: w.W, H: w.H} }

type resizeDirection uint8

const (
	resizeNone resizeDirection = iota
	resizeLeft
	resizeRight
	resizeTop
	resizeBottom
	resizeTopLeft
	resizeTopRight
	resizeBottomLeft
	resizeBottomRight
)

func (w *Window) resizeEdgeAt(x, y int) resizeDirection {
	if !w.Resizable || w.Maximized || w.Minimized || !w.Outer().Contains(x, y) {
		return resizeNone
	}
	const edge = 5
	left := x < w.X+edge
	right := x >= w.X+w.W-edge
	top := y < w.Y+edge
	bottom := y >= w.Y+w.H-edge
	switch {
	case left && top:
		return resizeTopLeft
	case right && top:
		return resizeTopRight
	case left && bottom:
		return resizeBottomLeft
	case right && bottom:
		return resizeBottomRight
	case left:
		return resizeLeft
	case right:
		return resizeRight
	case top:
		return resizeTop
	case bottom:
		return resizeBottom
	default:
		return resizeNone
	}
}

func (r resizeDirection) cursor() string {
	switch r {
	case resizeLeft, resizeRight:
		return "ew"
	case resizeTop, resizeBottom:
		return "ns"
	case resizeTopLeft, resizeBottomRight:
		return "nwse"
	case resizeTopRight, resizeBottomLeft:
		return "nesw"
	default:
		return ""
	}
}

func resizeWindowBounds(start ui.Rect, dir resizeDirection, dx, dy, minW, minH, maxW, maxH int) ui.Rect {
	r := start
	switch dir {
	case resizeLeft:
		x := start.X + dx
		x = maxInt(0, minInt(x, start.Right()-minW))
		r.X, r.W = x, start.Right()-x
	case resizeRight:
		r.W = maxInt(minW, minInt(start.W+dx, maxW-start.X))
	case resizeTop:
		y := start.Y + dy
		y = maxInt(0, minInt(y, start.Bottom()-minH))
		r.Y, r.H = y, start.Bottom()-y
	case resizeBottom:
		r.H = maxInt(minH, minInt(start.H+dy, maxH-start.Y))
	case resizeTopLeft:
		r = resizeWindowBounds(start, resizeLeft, dx, 0, minW, minH, maxW, maxH)
		r = resizeWindowBounds(r, resizeTop, 0, dy, minW, minH, maxW, maxH)
	case resizeTopRight:
		r = resizeWindowBounds(start, resizeRight, dx, 0, minW, minH, maxW, maxH)
		r = resizeWindowBounds(r, resizeTop, 0, dy, minW, minH, maxW, maxH)
	case resizeBottomLeft:
		r = resizeWindowBounds(start, resizeLeft, dx, 0, minW, minH, maxW, maxH)
		r = resizeWindowBounds(r, resizeBottom, 0, dy, minW, minH, maxW, maxH)
	case resizeBottomRight:
		r = resizeWindowBounds(start, resizeRight, dx, 0, minW, minH, maxW, maxH)
		r = resizeWindowBounds(r, resizeBottom, 0, dy, minW, minH, maxW, maxH)
	}
	return r
}
