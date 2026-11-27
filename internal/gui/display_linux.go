//go:build linux

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"win95linux/internal/desktop"
)

const (
	fbIOPutVar      = 0x4601
	fbActivateNow   = 0
	fbVmodeNoninter = 0
)

func framebufferRefresh(v *fbVarScreeninfo) int {
	if v.Pixclock == 0 {
		return 60
	}
	horizontal := uint64(v.LeftMargin) + uint64(v.Xres) + uint64(v.RightMargin) + uint64(v.HsyncLen)
	vertical := uint64(v.UpperMargin) + uint64(v.Yres) + uint64(v.LowerMargin) + uint64(v.VsyncLen)
	if horizontal == 0 || vertical == 0 {
		return 60
	}
	hz := 1000000000000 / (uint64(v.Pixclock) * horizontal * vertical)
	if hz < 24 || hz > 240 {
		return 60
	}
	return int(hz)
}

func refreshPixclock(v *fbVarScreeninfo, refresh int) uint32 {
	if v.Pixclock == 0 {
		return 0
	}
	horizontal := uint64(v.LeftMargin) + uint64(v.Xres) + uint64(v.RightMargin) + uint64(v.HsyncLen)
	vertical := uint64(v.UpperMargin) + uint64(v.Yres) + uint64(v.LowerMargin) + uint64(v.VsyncLen)
	if horizontal == 0 || vertical == 0 {
		return v.Pixclock
	}
	ps := 1000000000000 / (horizontal * vertical * uint64(refresh))
	if ps == 0 || ps > uint64(^uint32(0)) {
		return v.Pixclock
	}
	return uint32(ps)
}

func (f *fbDevice) displayMode(rotation int) desktop.DisplayMode {
	return desktop.DisplayMode{Width: f.w, Height: f.h, Refresh: framebufferRefresh(&f.varInfo), Rotation: rotation}.Normalized()
}

func parseDisplayGeometry(text string) (int, int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, 0, false
	}
	if i := strings.IndexAny(text, "pPiI-"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimLeft(text, "UVTDHM:")
	parts := strings.Split(strings.ToLower(text), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	return w, h, errW == nil && errH == nil && w >= 640 && h >= 480 && w <= 7680 && h <= 4320
}

func availableDisplayModes(f *fbDevice) []desktop.DisplayMode {
	seen := map[[2]int]bool{}
	modes := []desktop.DisplayMode{}
	add := func(w, h int) {
		key := [2]int{w, h}
		if !seen[key] {
			seen[key] = true
			modes = append(modes, desktop.DisplayMode{Width: w, Height: h, Refresh: 60})
		}
	}
	add(f.w, f.h)
	base := filepath.Base(f.path)
	if data, err := os.ReadFile("/sys/class/graphics/" + base + "/modes"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if w, h, ok := parseDisplayGeometry(line); ok {
				add(w, h)
			}
		}
	}
	paths, _ := filepath.Glob("/sys/class/drm/card*-*/modes")
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if w, h, ok := parseDisplayGeometry(line); ok {
				add(w, h)
			}
		}
	}
	for _, size := range [][2]int{{640, 480}, {800, 600}, {1024, 768}, {1280, 720}, {1280, 1024}, {1920, 1080}} {
		add(size[0], size[1])
	}
	sort.SliceStable(modes, func(i, j int) bool {
		if modes[i].Width == modes[j].Width {
			return modes[i].Height < modes[j].Height
		}
		return modes[i].Width < modes[j].Width
	})
	return modes
}

func (f *fbDevice) mappingSize() (int, error) {
	if f.w <= 0 || f.h <= 0 || f.lineBytes <= 0 {
		return 0, fmt.Errorf("invalid framebuffer geometry")
	}
	if f.lineBytes > int(^uint(0)>>1)/f.h {
		return 0, fmt.Errorf("framebuffer mapping size overflows")
	}
	need := f.lineBytes * f.h
	if f.fixInfo.SmemLen > 0 && int(f.fixInfo.SmemLen) > need {
		need = int(f.fixInfo.SmemLen)
	}
	if need <= 0 {
		return 0, fmt.Errorf("invalid framebuffer mapping size")
	}
	return need, nil
}

func (f *fbDevice) remap() error {
	if f.mm != nil {
		_ = syscall.Munmap(f.mm)
		f.mm = nil
	}
	need, err := f.mappingSize()
	if err != nil {
		return err
	}
	mm, err := syscall.Mmap(f.fd, 0, need, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return err
	}
	f.mm = mm
	return nil
}

func (f *fbDevice) restoreVar(v fbVarScreeninfo, fix fbFixScreeninfo) error {
	putErr := ioctlPtr(f.fd, fbIOPutVar, unsafe.Pointer(&v))
	f.varInfo, f.fixInfo = v, fix
	f.w, f.h = int(v.Xres), int(v.Yres)
	f.bpp = int(v.BitsPerPixel)
	f.lineBytes = int(fix.LineLength)
	f.direct = f.bpp == 32 && v.Red.Offset == 16 && v.Red.Length == 8 && v.Green.Offset == 8 && v.Green.Length == 8 && v.Blue.Offset == 0 && v.Blue.Length == 8
	f.stridePx = f.lineBytes / 4
	if mapErr := f.remap(); mapErr != nil {
		if putErr != nil {
			return fmt.Errorf("%v; framebuffer remap failed: %w", putErr, mapErr)
		}
		return mapErr
	}
	return putErr
}

func (f *fbDevice) setDisplayMode(mode desktop.DisplayMode) (desktop.DisplayMode, error) {
	mode = mode.Normalized()
	oldVar, oldFix := f.varInfo, f.fixInfo
	next := oldVar
	next.Xres, next.Yres = uint32(mode.Width), uint32(mode.Height)
	if next.XresVirtual < next.Xres {
		next.XresVirtual = next.Xres
	}
	if next.YresVirtual < next.Yres {
		next.YresVirtual = next.Yres
	}
	next.Xoffset, next.Yoffset = 0, 0
	next.Pixclock = refreshPixclock(&next, mode.Refresh)
	next.Activate = fbActivateNow
	next.Vmode = fbVmodeNoninter
	next.Rotate = 0
	if err := ioctlPtr(f.fd, fbIOPutVar, unsafe.Pointer(&next)); err != nil {
		return f.displayMode(f.rotation), fmt.Errorf("FBIOPUT_VSCREENINFO: %w", err)
	}
	var got fbVarScreeninfo
	if err := ioctlPtr(f.fd, fbIOGetVar, unsafe.Pointer(&got)); err != nil {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("read mode failed: %v; rollback failed: %w", err, restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("read applied mode: %w", err)
	}
	var fix fbFixScreeninfo
	if err := ioctlPtr(f.fd, fbIOGetFix, unsafe.Pointer(&fix)); err != nil {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("read fixed info failed: %v; rollback failed: %w", err, restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("read fixed info: %w", err)
	}
	if int(got.Xres) != mode.Width || int(got.Yres) != mode.Height || fix.LineLength == 0 {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("driver returned %dx%d; rollback failed: %w", got.Xres, got.Yres, restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("driver returned %dx%d instead of %dx%d", got.Xres, got.Yres, mode.Width, mode.Height)
	}
	bytesPer := int(got.BitsPerPixel) / 8
	if got.BitsPerPixel != 16 && got.BitsPerPixel != 24 && got.BitsPerPixel != 32 || int(fix.LineLength) < int(got.Xres)*bytesPer {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("driver returned invalid depth %d or stride %d; rollback failed: %w", got.BitsPerPixel, fix.LineLength, restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("driver returned unsupported depth %d or invalid stride %d", got.BitsPerPixel, fix.LineLength)
	}
	if fix.SmemLen > 0 && uint64(fix.SmemLen) < uint64(fix.LineLength)*uint64(got.Yres) {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("driver returned undersized framebuffer; rollback failed: %w", restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("driver returned undersized framebuffer memory")
	}
	f.varInfo, f.fixInfo = got, fix
	f.w, f.h = mode.Width, mode.Height
	f.bpp = int(got.BitsPerPixel)
	f.lineBytes = int(fix.LineLength)
	f.direct = f.bpp == 32 && got.Red.Offset == 16 && got.Red.Length == 8 && got.Green.Offset == 8 && got.Green.Length == 8 && got.Blue.Offset == 0 && got.Blue.Length == 8
	f.stridePx = f.lineBytes / 4
	if err := f.remap(); err != nil {
		restoreErr := f.restoreVar(oldVar, oldFix)
		if restoreErr != nil {
			return f.displayMode(f.rotation), fmt.Errorf("mmap failed: %v; rollback failed: %w", err, restoreErr)
		}
		return f.displayMode(f.rotation), fmt.Errorf("mmap applied mode: %w", err)
	}
	f.rotation = mode.Rotation
	refresh := framebufferRefresh(&got)
	if got.Pixclock == 0 {
		refresh = mode.Refresh
	}
	return desktop.DisplayMode{Width: mode.Width, Height: mode.Height, Refresh: refresh, Rotation: mode.Rotation}, nil
}
