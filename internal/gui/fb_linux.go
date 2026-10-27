//go:build linux

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"win95linux/internal/ui"
)

const (
	fbIOGetVar = 0x4600
	fbIOGetFix = 0x4602

	kdSetMode  = 0x4B3A
	kdText     = 0
	kdGraphics = 1
)

type fbBitfield struct {
	Offset   uint32
	Length   uint32
	MsbRight uint32
}

type fbVarScreeninfo struct {
	Xres         uint32
	Yres         uint32
	XresVirtual  uint32
	YresVirtual  uint32
	Xoffset      uint32
	Yoffset      uint32
	BitsPerPixel uint32
	Grayscale    uint32
	Red          fbBitfield
	Green        fbBitfield
	Blue         fbBitfield
	Transp       fbBitfield
	Nonstd       uint32
	Activate     uint32
	Height       uint32
	Width        uint32
	AccelFlags   uint32
	Pixclock     uint32
	LeftMargin   uint32
	RightMargin  uint32
	UpperMargin  uint32
	LowerMargin  uint32
	HsyncLen     uint32
	VsyncLen     uint32
	Sync         uint32
	Vmode        uint32
	Rotate       uint32
	Colorspace   uint32
	Reserved     [4]uint32
}

type fbFixScreeninfo struct {
	ID         [16]byte
	SmemStart  uint64
	SmemLen    uint32
	Type       uint32
	TypeAux    uint32
	Visual     uint32
	Xpanstep   uint16
	Ypanstep   uint16
	Ywrapstep  uint16
	LineLength uint32
	MmioStart  uint64
	MmioLen    uint32
	Accel      uint32
	Reserved   [3]uint16
}

type fbDevice struct {
	path      string
	fd        int
	w, h      int
	bpp       int
	lineBytes int
	mm        []byte
	varInfo   fbVarScreeninfo
	fixInfo   fbFixScreeninfo
	direct    bool
	stridePx  int
}

func ioctlPtr(fd int, req uintptr, ptr unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlVal(fd int, req uintptr, val uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, val)
	if errno != 0 {
		return errno
	}
	return nil
}

func openFramebuffer(path string) (*fbDevice, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := &fbDevice{fd: fd, path: path}
	if err := ioctlPtr(fd, fbIOGetVar, unsafe.Pointer(&f.varInfo)); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %w", err)
	}
	if err := ioctlPtr(fd, fbIOGetFix, unsafe.Pointer(&f.fixInfo)); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %w", err)
	}
	v := &f.varInfo
	f.w = int(v.Xres)
	f.h = int(v.Yres)
	f.bpp = int(v.BitsPerPixel)
	f.lineBytes = int(f.fixInfo.LineLength)
	if f.w <= 0 || f.h <= 0 || f.lineBytes <= 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("framebuffer geometry %dx%d line=%d is invalid", f.w, f.h, f.lineBytes)
	}
	switch f.bpp {
	case 16, 24, 32:
	default:
		syscall.Close(fd)
		return nil, fmt.Errorf("unsupported framebuffer depth %d", f.bpp)
	}
	f.direct = f.bpp == 32 &&
		v.Red.Offset == 16 && v.Red.Length == 8 &&
		v.Green.Offset == 8 && v.Green.Length == 8 &&
		v.Blue.Offset == 0 && v.Blue.Length == 8
	f.stridePx = f.lineBytes / 4
	need := f.lineBytes * f.h
	if f.fixInfo.SmemLen > 0 && int(f.fixInfo.SmemLen) > need {
		need = int(f.fixInfo.SmemLen)
	}
	mm, err := syscall.Mmap(fd, 0, need, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("mmap framebuffer: %w", err)
	}
	f.mm = mm
	return f, nil
}

func (f *fbDevice) close() {
	if f.mm != nil {
		syscall.Munmap(f.mm)
		f.mm = nil
	}
	if f.fd >= 0 {
		syscall.Close(f.fd)
		f.fd = -1
	}
}

func scaleTo(v uint32, bits uint32) uint32 {
	if bits == 0 {
		return 0
	}
	if bits >= 8 {
		return v >> (bits - 8)
	}
	max := uint32(1)<<bits - 1
	return (v*max + 127) / 255
}

func (f *fbDevice) packColor(col uint32) uint32 {
	r := (col >> 16) & 0xff
	g := (col >> 8) & 0xff
	b := col & 0xff
	v := &f.varInfo
	return scaleTo(r, v.Red.Length)<<v.Red.Offset |
		scaleTo(g, v.Green.Length)<<v.Green.Offset |
		scaleTo(b, v.Blue.Length)<<v.Blue.Offset |
		scaleTo(0, v.Transp.Length)<<v.Transp.Offset
}

// blit copies a 32-bit XRGB canvas into the framebuffer's native format.
func (f *fbDevice) blit(c *ui.Canvas) {
	if f.direct {
		dst := unsafe.Slice((*uint32)(unsafe.Pointer(&f.mm[0])), len(f.mm)/4)
		xoff := int(f.varInfo.Xoffset)
		yoff := int(f.varInfo.Yoffset)
		for y := 0; y < c.H && y < f.h; y++ {
			src := c.Pix[y*c.Stride : y*c.Stride+c.W]
			off := (y+yoff)*f.stridePx + xoff
			if off+len(src) > len(dst) {
				break
			}
			copy(dst[off:off+len(src)], src)
		}
		return
	}
	bytesPer := f.bpp / 8
	xoff := int(f.varInfo.Xoffset)
	yoff := int(f.varInfo.Yoffset)
	for y := 0; y < c.H && y < f.h; y++ {
		rowBase := (y + yoff) * f.lineBytes
		for x := 0; x < c.W && x < f.w; x++ {
			off := rowBase + (x+xoff)*bytesPer
			if off+bytesPer > len(f.mm) {
				break
			}
			packed := f.packColor(c.Pix[y*c.Stride+x])
			for i := 0; i < bytesPer; i++ {
				f.mm[off+i] = byte(packed >> (8 * i))
			}
		}
	}
}

func fbDriver(path string) string {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "fb") {
		return ""
	}
	b, err := os.ReadFile("/sys/class/graphics/" + base + "/name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func findFramebuffer() (*fbDevice, error) {
	priority := func(p string) int {
		name := fbDriver(p)
		switch {
		case strings.Contains(name, "virtio"):
			return 0
		case strings.Contains(name, "simple"), strings.Contains(name, "drm"):
			return 1
		case strings.Contains(name, "efi"):
			return 2
		default:
			return 3
		}
	}
	var lastErr error
	deadline := time.Now().Add(30 * time.Second)
	lastNote := time.Now()
	for time.Now().Before(deadline) {
		paths, _ := filepath.Glob("/dev/fb*")
		if len(paths) == 0 {
			paths = []string{"/dev/fb0"}
		}
		sort.SliceStable(paths, func(i, j int) bool { return priority(paths[i]) < priority(paths[j]) })
		for _, p := range paths {
			fb, err := openFramebuffer(p)
			if err == nil {
				logf("using framebuffer %s driver=%q priority=%d", p, fbDriver(p), priority(p))
				return fb, nil
			}
			lastErr = err
		}
		if time.Since(lastNote) > 5*time.Second {
			lastNote = time.Now()
			msg := "no /dev/fb0 appeared"
			if lastErr != nil {
				msg = lastErr.Error()
			}
			logf("waiting for display device /dev/fb0 ... (%s)", msg)
		}
		time.Sleep(120 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no framebuffer device appeared")
	}
	return nil, lastErr
}

func setConsoleQuiet(vt *vtController) {
	// Keep kernel messages and the fb cursor out of the GUI.
	_ = os.WriteFile("/proc/sys/kernel/printk", []byte("1 1 1 1"), 0o644)
	_ = os.WriteFile("/sys/class/graphics/fbcon/cursor_blink", []byte("0"), 0o644)
	if vt != nil {
		vt.setGraphics(true)
	}
}

// setConsoleText restores the VT so a failed GUI leaves a readable console.
func setConsoleText(vt *vtController) {
	if vt != nil {
		vt.setGraphics(false)
	}
	_ = os.WriteFile("/proc/sys/kernel/printk", []byte("4 4 1 7"), 0o644)
}
