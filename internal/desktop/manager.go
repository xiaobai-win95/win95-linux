package desktop

import (
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"win95linux/internal/ui"
)

type Desktop struct {
	W, H int
	Mode Mode
	Host Host

	windows []*Window
	active  *Window
	modal   *Window
	modals  []*Window

	MouseX, MouseY int
	mouseDown      bool
	capture        *Window
	hand           bool

	startOpen  bool
	startHover string
	startSub   string
	startSel   string

	icons         []*desktopIcon
	baseIcons     []*desktopIcon
	selected      int
	lastClick     time.Time
	lastIcon      int
	autoArrange   bool
	iconPositions map[string]iconPoint
	dragIcon      int
	dragIconDX    int
	dragIconDY    int
	dragStartX    int
	dragStartY    int
	dragIconMoved bool
	ctx           *contextMenu
	installer     *InstallerApp
	dirty         int32
	clock         string
	clockLoc      *time.Location
	lastSecond    int64

	cpuPct, memPct int
	cpuTotal       uint64
	cpuIdle        uint64

	lastInput    time.Time
	saver        bool
	saverAt      time.Time
	saverStep    time.Time
	saverTrails  []saverTrail
	shortcutMod  time.Time
	shortcutNext time.Time

	sleepPending bool
	sleeping     bool

	bsod     bool
	bsodAt   time.Time
	bsodStep time.Time

	postMu sync.Mutex
	postQ  []func()
}

func New(width, height int, mode Mode, host Host) *Desktop {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.Local
	}
	d := &Desktop{
		W:             width,
		H:             height,
		Mode:          mode,
		Host:          host,
		MouseX:        width / 2,
		MouseY:        height / 2,
		selected:      -1,
		lastIcon:      -1,
		dragIcon:      -1,
		autoArrange:   true,
		iconPositions: map[string]iconPoint{},
		clockLoc:      loc,
		lastInput:     time.Now(),
	}
	d.clock = time.Now().In(loc).Format("15:04")
	d.sampleSystem()
	d.setupShell()
	d.Invalidate()
	return d
}

func (d *Desktop) Invalidate() {
	atomic.StoreInt32(&d.dirty, 1)
}

// Post schedules f on the UI thread and wakes the host event loop.
func (d *Desktop) Post(f func()) {
	if f == nil {
		return
	}
	d.postMu.Lock()
	d.postQ = append(d.postQ, f)
	d.postMu.Unlock()
	if d.Host.Wake != nil {
		d.Host.Wake()
	}
}

// RunPosted drains callbacks queued by worker goroutines.
func (d *Desktop) RunPosted() {
	for {
		d.postMu.Lock()
		if len(d.postQ) == 0 {
			d.postMu.Unlock()
			return
		}
		f := d.postQ[0]
		d.postQ = d.postQ[1:]
		d.postMu.Unlock()
		f()
	}
}

func (d *Desktop) NeedsRedraw() bool {
	return atomic.SwapInt32(&d.dirty, 0) != 0
}

func (d *Desktop) taskbarTop() int { return d.H - taskbarH }

func (d *Desktop) workspace() ui.Rect {
	return ui.Rect{0, 0, d.W, d.taskbarTop()}
}

func (d *Desktop) Open(app App) *Window {
	if app == nil {
		return nil
	}
	w := &Window{
		App:       app,
		MinW:      220,
		MinH:      140,
		Resizable: true,
		menuOpen:  -1,
		created:   time.Now(),
	}
	if s, ok := app.(WindowSizer); ok {
		w.W, w.H = s.DefaultSize()
		w.MinW, w.MinH = s.MinSize()
		w.Resizable = s.Resizable()
	} else {
		w.W, w.H = 640, 440
	}
	if w.W > d.W-20 {
		w.W = d.W - 20
	}
	if w.H > d.taskbarTop()-20 {
		w.H = d.taskbarTop() - 20
	}
	w.X = (d.W - w.W) / 2
	w.Y = (d.taskbarTop() - w.H) / 2
	n := 0
	for _, other := range d.windows {
		if !other.Modal {
			n++
		}
	}
	w.X += (n % 6) * 24
	w.Y += (n % 6) * 20
	if w.X+w.W > d.W-8 {
		w.X = d.W - w.W - 8
	}
	if w.Y+w.H > d.taskbarTop()-8 {
		w.Y = d.taskbarTop() - w.H - 8
	}
	if w.X < 8 {
		w.X = 8
	}
	if w.Y < 8 {
		w.Y = 8
	}
	if w.Modal {
		w.X = (d.W - w.W) / 2
		w.Y = (d.taskbarTop() - w.H) / 2
	}
	d.windows = append(d.windows, w)
	d.refreshMenus(w)
	if w.Modal {
		d.modal = w
		d.startOpen = false
	}
	d.active = w
	d.closeMenusExcept(w)
	d.Invalidate()
	if client := w.ClientRect(); client.W > 0 && client.H > 0 {
		w.App.Resized(d, w, client)
	}
	return w
}

// OpenModal opens a dialog, dims nothing (classic Win95) and blocks the rest
// of the desktop until it is closed.
func (d *Desktop) OpenModal(app App) *Window {
	prev := d.active
	w := d.Open(app)
	w.Modal = true
	w.Resizable = false
	w.prevActive = prev
	w.X = (d.W - w.W) / 2
	w.Y = (d.taskbarTop() - w.H) / 2
	if w.Y < 4 {
		w.Y = 4
	}
	d.modal = w
	d.modals = append(d.modals, w)
	d.startOpen = false
	d.startSub = ""
	d.active = w
	return w
}

// WindowSizer lets an application choose its initial size and constraints.
type WindowSizer interface {
	DefaultSize() (w, h int)
	MinSize() (w, h int)
	Resizable() bool
}

type Ticker interface {
	Tick(d *Desktop, w *Window, now time.Time)
}

func (d *Desktop) Tick(now time.Time) {
	clock := now.In(d.clockLoc).Format("15:04")
	if clock != d.clock {
		d.clock = clock
		d.Invalidate()
	}
	sec := now.Unix()
	if sec != d.lastSecond {
		d.lastSecond = sec
		oldCPU, oldMem := d.cpuPct, d.memPct
		d.sampleSystem()
		if d.cpuPct != oldCPU || d.memPct != oldMem {
			d.Invalidate()
		}
		d.refreshShortcuts(false)
		for _, w := range d.windows {
			if t, ok := w.App.(Ticker); ok {
				t.Tick(d, w, now)
			}
		}
	}
}

// Animate advances time-based visual effects. The host calls it on a short
// frame ticker; the desktop stays mostly idle when nothing is animating.
func (d *Desktop) Animate(now time.Time) {
	if d.bsod {
		// Redraw slowly so the "Press any key" prompt keeps blinking.
		if now.Sub(d.bsodStep) >= 400*time.Millisecond {
			d.bsodStep = now
			d.Invalidate()
		}
		return
	}
	if d.sleepPending {
		d.runSleep()
		return
	}
	if d.saver {
		if now.Sub(d.saverStep) < 33*time.Millisecond {
			return
		}
		d.stepSaver(now)
		d.Invalidate()
		return
	}
	if !d.lastInput.IsZero() && now.Sub(d.lastInput) >= saverIdle {
		d.startSaver(now)
		d.Invalidate()
	}
}

func (d *Desktop) bringToFront(w *Window) {
	if w == nil {
		return
	}
	if w.Minimized {
		w.Minimized = false
	}
	out := make([]*Window, 0, len(d.windows))
	for _, o := range d.windows {
		if o != w {
			out = append(out, o)
		}
	}
	out = append(out, w)
	d.windows = out
	d.active = w
	d.Invalidate()
}

func (d *Desktop) closeMenusExcept(keep *Window) {
	for _, w := range d.windows {
		if w != keep {
			w.menuOpen = -1
		}
	}
}

func (d *Desktop) removeWindow(w *Window, callClose bool) {
	if callClose {
		w.App.Close()
	}
	for i, o := range d.windows {
		if o == w {
			d.windows = append(d.windows[:i], d.windows[i+1:]...)
			break
		}
	}
	if d.modal == w {
		for i := len(d.modals) - 1; i >= 0; i-- {
			if d.modals[i] == w {
				d.modals = append(d.modals[:i], d.modals[i+1:]...)
				break
			}
		}
		d.modal = nil
		if len(d.modals) > 0 {
			d.modal = d.modals[len(d.modals)-1]
		}
	}
	if d.capture == w {
		d.capture = nil
	}
	if d.active == w {
		d.active = d.modal
		if d.active == nil && w.prevActive != nil {
			found := false
			for _, o := range d.windows {
				if o == w.prevActive {
					found = true
					break
				}
			}
			if found {
				d.active = w.prevActive
			}
		}
		if d.active == nil {
			for i := len(d.windows) - 1; i >= 0; i-- {
				c := d.windows[i]
				if !c.Minimized && !c.Modal {
					d.active = c
					break
				}
			}
		}
	}
	d.Invalidate()
}

func (d *Desktop) CloseWindow(w *Window) {
	if w == nil {
		return
	}
	if r, ok := w.App.(CloseRequester); ok {
		if !r.RequestClose(d, w) {
			return
		}
	}
	d.removeWindow(w, true)
}

func (d *Desktop) CloseWindowForced(w *Window) {
	d.removeWindow(w, true)
}

func (d *Desktop) Windows() []*Window { return d.windows }

func (d *Desktop) Active() *Window { return d.active }

func (d *Desktop) SetHand(v bool) { d.hand = v }

func (d *Desktop) refreshMenus(w *Window) {
	if p, ok := w.App.(MenuProvider); ok {
		w.menus = p.Menus(d, w)
	} else {
		w.menus = nil
	}
	if w.menuOpen >= len(w.menus) {
		w.menuOpen = -1
	}
}

func (d *Desktop) frameIcon(w *Window) ui.IconKind { return w.App.Icon() }

func (d *Desktop) MenuHeight(w *Window) int {
	if len(w.menus) > 0 {
		return menuBarH
	}
	return 0
}
