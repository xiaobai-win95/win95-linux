//go:build linux

package gui

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"win95linux/internal/desktop"
)

const (
	evSyn = 0
	evKey = 1
	evRel = 2
	evAbs = 3

	btnLeft  = 0x110
	btnRight = 0x111
	btnTouch = 0x14a
	relWheel = 8
)

type inputDevice struct {
	fd        int
	path      string
	name      string
	keyboard  bool
	pointer   bool
	absMaxX   int32
	absMaxY   int32
	closeOnce sync.Once
}

type inputEvent struct {
	dev  *inputDevice
	typ  uint16
	code uint16
	val  int32
}

func iocRead(typ, nr, size uintptr) uintptr {
	return 2<<30 | size<<16 | typ<<8 | nr
}

func ioctlName(fd int) string {
	buf := make([]byte, 256)
	req := iocRead('E', 0x06, uintptr(len(buf)))
	n, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	if int(n) > len(buf) {
		n = uintptr(len(buf))
	}
	return strings.TrimRight(string(buf[:n]), "\x00")
}

func ioctlKeyBits(fd int) []byte {
	buf := make([]byte, 128)
	req := iocRead('E', 0x20+evKey, uintptr(len(buf)))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil
	}
	return buf
}

func bitSet(bits []byte, n int) bool {
	if n < 0 || n/8 >= len(bits) {
		return false
	}
	return bits[n/8]&(1<<uint(n%8)) != 0
}

type absInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

func ioctlAbs(fd int, axis uintptr) (absInfo, bool) {
	var a absInfo
	req := iocRead('E', 0x40+axis, unsafe.Sizeof(a))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&a)))
	if errno != 0 {
		return a, false
	}
	return a, true
}

func discoverInputs() []*inputDevice {
	paths, _ := filepath.Glob("/dev/input/event*")
	var out []*inputDevice
	for _, path := range paths {
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		d := &inputDevice{fd: fd, path: path, name: ioctlName(fd)}
		keys := ioctlKeyBits(fd)
		d.keyboard = bitSet(keys, 30) || bitSet(keys, 28)
		d.pointer = bitSet(keys, btnLeft) || bitSet(keys, btnTouch)
		if ax, ok := ioctlAbs(fd, 0); ok && ax.Maximum > 0 {
			d.absMaxX = ax.Maximum
		}
		if ay, ok := ioctlAbs(fd, 1); ok && ay.Maximum > 0 {
			d.absMaxY = ay.Maximum
		}
		if d.absMaxX > 0 && d.absMaxY > 0 {
			d.pointer = true
		}
		if !d.keyboard && !d.pointer {
			syscall.Close(fd)
			continue
		}
		out = append(out, d)
	}
	return out
}

func waitForInputs() []*inputDevice {
	deadline := time.Now().Add(6 * time.Second)
	for {
		devs := discoverInputs()
		keyboard, pointer := false, false
		for _, d := range devs {
			keyboard = keyboard || d.keyboard
			pointer = pointer || d.pointer
		}
		if keyboard && pointer {
			return devs
		}
		if time.Now().After(deadline) {
			return devs
		}
		for _, d := range devs {
			d.close()
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (d *inputDevice) readLoop(ch chan<- inputEvent) {
	buf := make([]byte, 24*64)
	for {
		n, err := syscall.Read(d.fd, buf)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(3 * time.Millisecond)
				continue
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			logf("input %s stopped: %v", d.path, err)
			return
		}
		if n <= 0 {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		for off := 0; off+24 <= n; off += 24 {
			typ := binary.LittleEndian.Uint16(buf[off+16:])
			code := binary.LittleEndian.Uint16(buf[off+18:])
			val := int32(binary.LittleEndian.Uint32(buf[off+20:]))
			if typ != evKey && typ != evAbs && typ != evRel {
				continue
			}
			select {
			case ch <- inputEvent{dev: d, typ: typ, code: code, val: val}:
			default:
			}
		}
	}
}

func (d *inputDevice) close() {
	d.closeOnce.Do(func() {
		if d.fd >= 0 {
			syscall.Close(d.fd)
			d.fd = -1
		}
	})
}

type keyDef struct {
	name  string
	base  rune
	shift rune
}

// Linux evdev key codes for a US keyboard. Printable keys carry both the
// unshifted and shifted character; special keys only carry a name.
var keyTable = map[uint16]keyDef{
	1:   {name: "Esc"},
	2:   {base: '1', shift: '!'},
	3:   {base: '2', shift: '@'},
	4:   {base: '3', shift: '#'},
	5:   {base: '4', shift: '$'},
	6:   {base: '5', shift: '%'},
	7:   {base: '6', shift: '^'},
	8:   {base: '7', shift: '&'},
	9:   {base: '8', shift: '*'},
	10:  {base: '9', shift: '('},
	11:  {base: '0', shift: ')'},
	12:  {base: '-', shift: '_'},
	13:  {base: '=', shift: '+'},
	14:  {name: "Backspace"},
	15:  {name: "Tab"},
	16:  {base: 'q'},
	17:  {base: 'w'},
	18:  {base: 'e'},
	19:  {base: 'r'},
	20:  {base: 't'},
	21:  {base: 'y'},
	22:  {base: 'u'},
	23:  {base: 'i'},
	24:  {base: 'o'},
	25:  {base: 'p'},
	26:  {base: '[', shift: '{'},
	27:  {base: ']', shift: '}'},
	28:  {name: "Enter"},
	29:  {name: "Ctrl"},
	30:  {base: 'a'},
	31:  {base: 's'},
	32:  {base: 'd'},
	33:  {base: 'f'},
	34:  {base: 'g'},
	35:  {base: 'h'},
	36:  {base: 'j'},
	37:  {base: 'k'},
	38:  {base: 'l'},
	39:  {base: ';', shift: ':'},
	40:  {base: '\'', shift: '"'},
	41:  {base: '`', shift: '~'},
	42:  {name: "Shift"},
	43:  {base: '\\', shift: '|'},
	44:  {base: 'z'},
	45:  {base: 'x'},
	46:  {base: 'c'},
	47:  {base: 'v'},
	48:  {base: 'b'},
	49:  {base: 'n'},
	50:  {base: 'm'},
	51:  {base: ',', shift: '<'},
	52:  {base: '.', shift: '>'},
	53:  {base: '/', shift: '?'},
	54:  {name: "Shift"},
	55:  {base: '*', shift: '*'},
	56:  {name: "Alt"},
	57:  {base: ' ', shift: ' '},
	58:  {name: "CapsLock"},
	59:  {name: "F1"},
	60:  {name: "F2"},
	61:  {name: "F3"},
	62:  {name: "F4"},
	63:  {name: "F5"},
	64:  {name: "F6"},
	65:  {name: "F7"},
	66:  {name: "F8"},
	67:  {name: "F9"},
	68:  {name: "F10"},
	69:  {name: "NumLock"},
	70:  {name: "ScrollLock"},
	71:  {base: '7', shift: '7'},
	72:  {base: '8', shift: '8'},
	73:  {base: '9', shift: '9'},
	74:  {base: '-', shift: '-'},
	75:  {base: '4', shift: '4'},
	76:  {base: '5', shift: '5'},
	77:  {base: '6', shift: '6'},
	78:  {base: '+', shift: '+'},
	79:  {base: '1', shift: '1'},
	80:  {base: '2', shift: '2'},
	81:  {base: '3', shift: '3'},
	82:  {base: '0', shift: '0'},
	83:  {base: '.', shift: '.'},
	87:  {name: "F11"},
	88:  {name: "F12"},
	96:  {name: "Enter"},
	97:  {name: "Ctrl"},
	98:  {base: '/', shift: '/'},
	99:  {name: "Print"},
	100: {name: "Alt"},
	102: {name: "Home"},
	103: {name: "Up"},
	104: {name: "PgUp"},
	105: {name: "Left"},
	106: {name: "Right"},
	107: {name: "End"},
	108: {name: "Down"},
	109: {name: "PgDn"},
	110: {name: "Insert"},
	111: {name: "Delete"},
	119: {name: "Pause"},
	125: {name: "Win"},
	126: {name: "Win"},
	127: {name: "Menu"},
}

type inputState struct {
	fbW, fbH int
	shift    bool
	ctrl     bool
	alt      bool
	caps     bool
	haveX    bool
	haveY    bool
}

func newInputState(w, h int) *inputState {
	return &inputState{fbW: w, fbH: h}
}

// resetModifiers drops keys that may still have been held when the VT was
// switched away; their release events are not delivered to us.
func (s *inputState) resetModifiers() {
	s.shift = false
	s.ctrl = false
	s.alt = false
}

func (s *inputState) clampX(x int) int {
	if x < 0 {
		return 0
	}
	if x >= s.fbW {
		return s.fbW - 1
	}
	return x
}

func (s *inputState) clampY(y int) int {
	if y < 0 {
		return 0
	}
	if y >= s.fbH {
		return s.fbH - 1
	}
	return y
}

func (s *inputState) move(d *desktop.Desktop, x, y int) {
	d.Mouse(desktop.MouseEvent{Kind: desktop.MouseMove, X: s.clampX(x), Y: s.clampY(y)})
}

func (s *inputState) handle(ev inputEvent, d *desktop.Desktop) {
	switch ev.typ {
	case evKey:
		switch ev.code {
		case 42, 54:
			s.shift = ev.val != 0
			return
		case 29, 97:
			s.ctrl = ev.val != 0
			return
		case 56, 100:
			s.alt = ev.val != 0
			return
		case 58:
			if ev.val == 1 {
				s.caps = !s.caps
			}
			return
		}
		if ev.code == btnLeft || ev.code == btnTouch {
			switch ev.val {
			case 1:
				d.Mouse(desktop.MouseEvent{Kind: desktop.MouseDown, Button: 1, X: d.MouseX, Y: d.MouseY, Shift: s.shift, Ctrl: s.ctrl})
			case 0:
				d.Mouse(desktop.MouseEvent{Kind: desktop.MouseUp, Button: 1, X: d.MouseX, Y: d.MouseY, Shift: s.shift, Ctrl: s.ctrl})
			}
			return
		}
		if ev.code == btnRight {
			switch ev.val {
			case 1:
				d.Mouse(desktop.MouseEvent{Kind: desktop.MouseDown, Button: 3, X: d.MouseX, Y: d.MouseY, Shift: s.shift, Ctrl: s.ctrl})
			case 0:
				d.Mouse(desktop.MouseEvent{Kind: desktop.MouseUp, Button: 3, X: d.MouseX, Y: d.MouseY, Shift: s.shift, Ctrl: s.ctrl})
			}
			return
		}
		if ev.val != 1 && ev.val != 2 {
			return
		}
		kd, ok := keyTable[ev.code]
		if !ok {
			return
		}
		ke := desktop.KeyEvent{Shift: s.shift, Ctrl: s.ctrl, Alt: s.alt}
		if kd.name != "" {
			ke.Name = kd.name
		}
		if kd.base != 0 {
			r := kd.base
			if kd.base >= 'a' && kd.base <= 'z' {
				if s.shift != s.caps {
					r = unicode.ToUpper(kd.base)
				}
			} else if s.shift && kd.shift != 0 {
				r = kd.shift
			}
			ke.Rune = r
			if ke.Name == "" {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					ke.Name = strings.ToUpper(string(r))
				} else {
					ke.Name = string(kd.base)
				}
			}
		}
		d.Key(ke)
	case evAbs:
		switch ev.code {
		case 0:
			if ev.dev.absMaxX <= 0 {
				return
			}
			s.haveX = true
			x := int(int64(ev.val) * int64(s.fbW-1) / int64(ev.dev.absMaxX))
			s.move(d, x, d.MouseY)
		case 1:
			if ev.dev.absMaxY <= 0 {
				return
			}
			s.haveY = true
			y := int(int64(ev.val) * int64(s.fbH-1) / int64(ev.dev.absMaxY))
			s.move(d, d.MouseX, y)
		}
	case evRel:
		switch ev.code {
		case 0:
			s.move(d, d.MouseX+int(ev.val), d.MouseY)
		case 1:
			s.move(d, d.MouseX, d.MouseY+int(ev.val))
		case relWheel:
			if ev.val == 0 {
				return
			}
			d.Mouse(desktop.MouseEvent{Kind: desktop.MouseWheel, X: d.MouseX, Y: d.MouseY, Wheel: int(ev.val), Shift: s.shift, Ctrl: s.ctrl})
		}
	}
}
