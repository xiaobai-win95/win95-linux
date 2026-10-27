//go:build linux

package gui

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	vtGetMode  = 0x5601
	vtSetMode  = 0x5602
	vtGetState = 0x5603
	vtRelDisp  = 0x5605
	tiocgDev   = 0x80045432

	vtAuto    = 0
	vtProcess = 1
	vtAckAcq  = 2
)

// vtMode mirrors struct vt_mode from <linux/vt.h>.
type vtMode struct {
	Mode   uint8
	Waitv  uint8
	Relsig int16
	Acqsig int16
	Frsig  int16
}

var (
	_ [unsafe.Sizeof(vtMode{}) - 8]byte
	_ [8 - unsafe.Sizeof(vtMode{})]byte
)

type vtStat struct {
	Active uint16
	Signal uint16
	State  uint16
}

// vtController owns the VT handshake. In VT_AUTO mode the kernel silently
// ignores console switches while a VT is in KD_GRAPHICS, so chvt blocks
// forever in VT_WAITACTIVE. VT_PROCESS makes the kernel ask us first.
type vtController struct {
	f       *os.File
	orig    vtMode
	hasOrig bool
}

// vtOwnerAlive is never closed; receiving on it keeps the VT owner thread
// alive for the lifetime of the process.
var vtOwnerAlive = make(chan struct{})

// setVTMode must run on a locked OS thread: the kernel records the calling
// thread's TID in vt_pid and later sends SIGUSR1/SIGUSR2 to that TID. A Go
// goroutine can migrate or its thread can be retired, which would silently
// lose console-switch signals, so pin a dedicated thread and keep it alive.
func setVTMode(fd int, mode *vtMode) error {
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := ioctlPtr(fd, vtSetMode, unsafe.Pointer(mode)); err != nil {
			result <- err
			return
		}
		result <- nil
		<-vtOwnerAlive
	}()
	return <-result
}

func openVTController() (*vtController, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		f, err = os.OpenFile("/dev/tty1", os.O_RDWR|syscall.O_CLOEXEC, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("open VT: %w", err)
	}
	c := &vtController{f: f}
	if err := ioctlPtr(int(f.Fd()), vtGetMode, unsafe.Pointer(&c.orig)); err == nil {
		c.hasOrig = true
	}
	mode := vtMode{
		Mode:   vtProcess,
		Relsig: int16(syscall.SIGUSR1),
		Acqsig: int16(syscall.SIGUSR2),
	}
	if err := setVTMode(int(f.Fd()), &mode); err != nil {
		f.Close()
		return nil, fmt.Errorf("VT_SETMODE: %w", err)
	}
	return c, nil
}

func (c *vtController) ackRelease() {
	c.ack(1)
}

func (c *vtController) ackAcquire() {
	c.ack(vtAckAcq)
}

func (c *vtController) ack(value uintptr) {
	if c == nil || c.f == nil {
		return
	}
	_ = ioctlVal(int(c.f.Fd()), vtRelDisp, value)
}

// number reports which VT this fd refers to, so the GUI can start paused when
// it was respawned while another console was in the foreground.
func (c *vtController) number() int {
	if c == nil || c.f == nil {
		return 1
	}
	var dev uint32
	if err := ioctlPtr(int(c.f.Fd()), tiocgDev, unsafe.Pointer(&dev)); err != nil {
		return 1
	}
	major := (dev >> 8) & 0xfff
	minor := (dev & 0xff) | ((dev >> 12) & 0xfff00)
	if major == 4 && minor > 0 {
		return int(minor)
	}
	return 1
}

func (c *vtController) foreground() (int, bool) {
	if c == nil || c.f == nil {
		return 0, false
	}
	var st vtStat
	if err := ioctlPtr(int(c.f.Fd()), vtGetState, unsafe.Pointer(&st)); err != nil {
		return 0, false
	}
	return int(st.Active), true
}

// setGraphics switches this specific VT (not whichever one happens to be
// active) into graphics mode.
func (c *vtController) setGraphics(on bool) {
	if c == nil || c.f == nil {
		return
	}
	mode := uintptr(kdText)
	if on {
		mode = kdGraphics
	}
	_ = ioctlVal(int(c.f.Fd()), kdSetMode, mode)
}

// restore returns the VT to kernel control before the process exits.
func (c *vtController) restore() {
	if c == nil || c.f == nil {
		return
	}
	if c.hasOrig {
		_ = ioctlPtr(int(c.f.Fd()), vtSetMode, unsafe.Pointer(&c.orig))
	} else {
		auto := vtMode{Mode: vtAuto}
		_ = ioctlPtr(int(c.f.Fd()), vtSetMode, unsafe.Pointer(&auto))
	}
	c.f.Close()
	c.f = nil
}
