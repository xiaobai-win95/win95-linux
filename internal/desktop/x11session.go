package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

const (
	x11Display      = ":1"
	x11ScreenWidth  = 800
	x11ScreenHeight = 600
	x11Depth        = 24
)

var errX11Missing = errors.New("X11 虚拟显示组件尚未安装")

type x11AppMissingError struct {
	name string
}

func (e *x11AppMissingError) Error() string {
	return "没有找到 " + e.name
}

type x11KeyRef struct {
	code  byte
	shift bool
}

type x11Session struct {
	mu     sync.Mutex
	conn   *xgb.Conn
	root   xproto.Window
	width  int
	height int
	lsb    bool

	keys  map[xproto.Keysym]x11KeyRef
	shift byte
	ctrl  byte
	alt   byte
}

var (
	x11SessionMu sync.Mutex
	x11Shared    *x11Session
)

// x11EnsureSession connects to DISPLAY :1, starting Xvfb in the background
// when it is not running yet.
func x11EnsureSession() (*x11Session, error) {
	x11SessionMu.Lock()
	defer x11SessionMu.Unlock()
	if x11Shared != nil {
		if _, err := xproto.GetInputFocus(x11Shared.conn).Reply(); err == nil {
			return x11Shared, nil
		}
		x11Shared.conn.Close()
		x11Shared = nil
	}
	if err := x11StartXvfb(); err != nil {
		return nil, err
	}
	conn, err := xgb.NewConnDisplay(x11Display)
	if err != nil {
		return nil, fmt.Errorf("无法连接 %s：%w", x11Display, err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("X11 服务器缺少 XTest 扩展：%w", err)
	}
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	s := &x11Session{
		conn:   conn,
		root:   screen.Root,
		width:  int(screen.WidthInPixels),
		height: int(screen.HeightInPixels),
		lsb:    setup.ImageByteOrder == xproto.ImageOrderLSBFirst,
	}
	if s.width <= 0 || s.height <= 0 {
		s.width, s.height = x11ScreenWidth, x11ScreenHeight
	}
	s.loadKeymap(setup.MinKeycode, setup.MaxKeycode)
	s.focus()
	x11Shared = s
	return s, nil
}

// removeStaleXLock clears a lock left by a crashed Xvfb. Without this a new
// server refuses to start even though nothing is listening on :1.
func removeStaleXLock() {
	data, err := os.ReadFile("/tmp/.X1-lock")
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return
	}
	if err := syscall.Kill(pid, 0); err == nil || err == syscall.EPERM {
		return
	}
	_ = os.Remove("/tmp/.X1-lock")
	_ = os.Remove("/tmp/.X11-unix/X1")
}

func x11StartXvfb() error {
	if runtime.GOOS != "linux" {
		return errors.New("X11 嵌套代理只能在 Linux 客户机中运行")
	}
	if conn, err := xgb.NewConnDisplay(x11Display); err == nil {
		conn.Close()
		return nil
	}
	removeStaleXLock()
	bin, err := exec.LookPath("Xvfb")
	if err != nil {
		if st, statErr := os.Stat("/usr/bin/Xvfb"); statErr == nil && !st.IsDir() {
			bin = "/usr/bin/Xvfb"
		} else {
			return errX11Missing
		}
	}
	logFile, _ := os.OpenFile("/tmp/win95-xvfb.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	cmd := exec.Command(bin, x11Display, "-screen", "0", fmt.Sprintf("%dx%dx%d", x11ScreenWidth, x11ScreenHeight, x11Depth), "-nolisten", "tcp", "-ac")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("无法启动 Xvfb：%w", err)
	}
	_ = cmd.Process.Release()
	if logFile != nil {
		logFile.Close()
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := xgb.NewConnDisplay(x11Display); err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("Xvfb 启动超时，请查看 /tmp/win95-xvfb.log")
}

func (s *x11Session) loadKeymap(min, max xproto.Keycode) {
	if max < min {
		return
	}
	count := byte(max - min + 1)
	reply, err := xproto.GetKeyboardMapping(s.conn, min, count).Reply()
	if err != nil || reply == nil || reply.KeysymsPerKeycode == 0 {
		return
	}
	per := int(reply.KeysymsPerKeycode)
	s.keys = make(map[xproto.Keysym]x11KeyRef, len(reply.Keysyms))
	for i := 0; i < int(count); i++ {
		for level := 0; level < per && level < 4; level++ {
			idx := i*per + level
			if idx >= len(reply.Keysyms) {
				break
			}
			ks := reply.Keysyms[idx]
			if ks == 0 {
				continue
			}
			if _, exists := s.keys[ks]; exists {
				continue
			}
			s.keys[ks] = x11KeyRef{code: byte(int(min) + i), shift: level == 1 || level == 3}
		}
	}
	if k, ok := s.keys[xproto.Keysym(0xFFE1)]; ok { // Shift_L
		s.shift = k.code
	}
	if k, ok := s.keys[xproto.Keysym(0xFFE3)]; ok { // Control_L
		s.ctrl = k.code
	}
	if k, ok := s.keys[xproto.Keysym(0xFFE9)]; ok { // Alt_L
		s.alt = k.code
	}
}

// Capture grabs the whole :1 screen and converts it to 0xRRGGBB pixels.
func (s *x11Session) Capture() ([]uint32, uint64, error) {
	s.mu.Lock()
	conn, root, w, h, lsb := s.conn, s.root, s.width, s.height, s.lsb
	s.mu.Unlock()
	reply, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(root), 0, 0, uint16(w), uint16(h), ^uint32(0)).Reply()
	if err != nil {
		return nil, 0, err
	}
	if reply == nil || len(reply.Data) < w*h {
		return nil, 0, errors.New("XGetImage 返回的数据不完整")
	}
	return x11ConvertPixels(reply.Data, w, h, lsb)
}

// x11ConvertPixels converts a ZPixmap reply (24/32-bit) into 0xRRGGBB pixels
// and an FNV-1a hash used to skip repaints when nothing changed.
func x11ConvertPixels(data []byte, w, h int, lsb bool) ([]uint32, uint64, error) {
	if w <= 0 || h <= 0 || len(data) < w*h {
		return nil, 0, errors.New("XGetImage 返回的数据不完整")
	}
	stride := len(data) / h
	bpp := stride / w
	if bpp < 3 {
		return nil, 0, fmt.Errorf("不支持的 X11 像素格式：%d 字节/像素", bpp)
	}
	pix := make([]uint32, w*h)
	var hash uint64 = 1469598103934665603
	for y := 0; y < h; y++ {
		row := data[y*stride:]
		out := pix[y*w:]
		for x := 0; x < w; x++ {
			i := x * bpp
			var v uint32
			if lsb {
				v = uint32(row[i]) | uint32(row[i+1])<<8 | uint32(row[i+2])<<16
			} else if bpp == 4 {
				// Big-endian XRGB pads the leading byte.
				v = uint32(row[i+1])<<16 | uint32(row[i+2])<<8 | uint32(row[i+3])
			} else {
				v = uint32(row[i])<<16 | uint32(row[i+1])<<8 | uint32(row[i+2])
			}
			out[x] = v & 0xFFFFFF
			hash = (hash ^ uint64(out[x])) * 1099511628211
		}
	}
	return pix, hash, nil
}

func (s *x11Session) focus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	xproto.SetInputFocus(s.conn, xproto.InputFocusPointerRoot, xproto.Window(xproto.InputFocusPointerRoot), 0)
}

// fitWindows clamps oversized top-level windows to the virtual screen. Xvfb
// has no window manager, so applications otherwise keep their default
// 1024x768 size and important controls fall outside the captured area.
func (s *x11Session) fitWindows() {
	tree, err := xproto.QueryTree(s.conn, s.root).Reply()
	if err != nil || tree == nil {
		return
	}
	for _, win := range tree.Children {
		attr, err := xproto.GetWindowAttributes(s.conn, win).Reply()
		if err != nil || attr.MapState != xproto.MapStateViewable {
			continue
		}
		geo, err := xproto.GetGeometry(s.conn, xproto.Drawable(win)).Reply()
		if err != nil || geo == nil {
			continue
		}
		w, h := int(geo.Width), int(geo.Height)
		if w <= s.width && h <= s.height {
			continue
		}
		if w > s.width {
			w = s.width
		}
		if h > s.height {
			h = s.height
		}
		_ = xproto.ConfigureWindow(s.conn, win,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, 0, uint32(w), uint32(h)}).Check()
	}
}

func (s *x11Session) FakeInput(typ, detail byte, x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	xtest.FakeInput(s.conn, typ, detail, 0, s.root, int16(x), int16(y), 0)
}

func (s *x11Session) Move(x, y int) {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= s.width {
		x = s.width - 1
	}
	if y >= s.height {
		y = s.height - 1
	}
	s.FakeInput(xproto.MotionNotify, 0, x, y)
}

func (s *x11Session) Button(down bool, button byte, x, y int) {
	s.Move(x, y)
	typ := byte(xproto.ButtonPress)
	if !down {
		typ = byte(xproto.ButtonRelease)
	}
	s.FakeInput(typ, button, x, y)
}

func (s *x11Session) Wheel(x, y, delta int) {
	button := byte(4)
	if delta < 0 {
		button = 5
		delta = -delta
	}
	if delta > 6 {
		delta = 6
	}
	for i := 0; i < delta; i++ {
		s.Button(true, button, x, y)
		s.Button(false, button, x, y)
	}
}

func (s *x11Session) Key(ev KeyEvent) {
	ks := x11KeysymFor(ev)
	if ks == 0 {
		return
	}
	ref, ok := s.keys[ks]
	if !ok {
		if ev.Rune >= 'A' && ev.Rune <= 'Z' {
			if lower, found := s.keys[x11KeysymFor(KeyEvent{Rune: ev.Rune + 32})]; found {
				ref = lower
				ref.shift = true
				ok = true
			}
		}
	}
	if !ok {
		return
	}
	mods := make([]byte, 0, 3)
	if (ref.shift || ev.Shift) && s.shift != 0 {
		mods = append(mods, s.shift)
	}
	if ev.Ctrl && s.ctrl != 0 {
		mods = append(mods, s.ctrl)
	}
	if ev.Alt && s.alt != 0 {
		mods = append(mods, s.alt)
	}
	for _, m := range mods {
		s.FakeInput(xproto.KeyPress, m, 0, 0)
	}
	s.FakeInput(xproto.KeyPress, ref.code, 0, 0)
	s.FakeInput(xproto.KeyRelease, ref.code, 0, 0)
	for i := len(mods) - 1; i >= 0; i-- {
		s.FakeInput(xproto.KeyRelease, mods[i], 0, 0)
	}
}

func x11KeysymFor(ev KeyEvent) xproto.Keysym {
	if ev.Name != "" {
		if ks, ok := x11NamedKeysyms[ev.Name]; ok {
			return ks
		}
	}
	if ev.Rune >= 32 && ev.Rune <= 0x10FFFF {
		if ev.Rune <= 0xFF {
			return xproto.Keysym(ev.Rune)
		}
		return xproto.Keysym(0x01000000 | uint32(ev.Rune))
	}
	return 0
}

var x11NamedKeysyms = map[string]xproto.Keysym{
	"Esc":       0xFF1B,
	"Backspace": 0xFF08,
	"Tab":       0xFF09,
	"Enter":     0xFF0D,
	"Home":      0xFF50,
	"Left":      0xFF51,
	"Up":        0xFF52,
	"Right":     0xFF53,
	"Down":      0xFF54,
	"PgUp":      0xFF55,
	"PgDn":      0xFF56,
	"End":       0xFF57,
	"Insert":    0xFF63,
	"Delete":    0xFFFF,
	"Pause":     0xFF13,
	"Print":     0xFF61,
	"Menu":      0xFF67,
	"Win":       0xFFEB,
	"F1":        0xFFBE,
	"F2":        0xFFBF,
	"F3":        0xFFC0,
	"F4":        0xFFC1,
	"F5":        0xFFC2,
	"F6":        0xFFC3,
	"F7":        0xFFC4,
	"F8":        0xFFC5,
	"F9":        0xFFC6,
	"F10":       0xFFC7,
	"F11":       0xFFC8,
	"F12":       0xFFC9,
}

// Launch starts an X11 application on :1, detached from the Win95 process.
func (s *x11Session) Launch(app string, extra ...string) error {
	exe, err := exec.LookPath(app)
	if err != nil {
		return &x11AppMissingError{name: app}
	}
	args := append([]string{}, extra...)
	cmd := exec.Command(exe, args...)
	cmd.Dir = "/root"
	_ = os.MkdirAll("/run/user/0", 0o700)
	_ = os.Chmod("/run/user/0", 0o700)
	cmd.Env = append(os.Environ(),
		"DISPLAY="+x11Display,
		"WIN95_NESTED_X11=1",
		"HOME=/root",
		"XDG_RUNTIME_DIR=/run/user/0",
		"XDG_SESSION_TYPE=x11",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=zh_CN.UTF-8",
		"LANGUAGE=zh_CN:zh",
		"LIBGL_ALWAYS_SOFTWARE=1",
		"GALLIUM_DRIVER=llvmpipe",
		"MOZ_DISABLE_CONTENT_SANDBOX=1",
		"MOZ_ENABLE_WAYLAND=0",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logPath := filepath.Join("/tmp", "win95-x11-app.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logFile != nil {
		fmt.Fprintf(logFile, "\n=== %s %s ===\n", time.Now().Format(time.RFC3339), strings.Join(append([]string{app}, args...), " "))
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return err
	}
	_ = cmd.Process.Release()
	if logFile != nil {
		logFile.Close()
	}
	return nil
}
