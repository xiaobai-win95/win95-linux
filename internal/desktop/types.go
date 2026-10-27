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

// Host connects the UI to machine-level actions such as power management.
type Host struct {
	Reboot   func()
	PowerOff func()
	Logout   func()
	// Suspend blocks until the machine wakes from sleep. The desktop has
	// already painted its sleep screen when this is called.
	Suspend func() error
	// Wake asks the host event loop to process queued UI callbacks.
	Wake func()
	Info string
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
	restore    ui.Rect

	menus      []Menu
	menuOpen   int
	menuHover  int
	pressedBtn string
	drag       bool
	dragDX     int
	dragDY     int
	resizing   bool
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
	sysTrayW   = 104
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
