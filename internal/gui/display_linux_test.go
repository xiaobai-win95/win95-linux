//go:build linux

package gui

import (
	"testing"

	"win95linux/internal/desktop"
)

func TestRefreshPixclockUsesRequestedGeometry(t *testing.T) {
	v := fbVarScreeninfo{
		Xres: 800, Yres: 600, Pixclock: 40000,
		LeftMargin: 40, RightMargin: 40, HsyncLen: 32,
		UpperMargin: 10, LowerMargin: 10, VsyncLen: 8,
	}
	next := v
	next.Xres, next.Yres = 1280, 720
	next.Pixclock = refreshPixclock(&next, 75)
	if got := framebufferRefresh(&next); got != 75 {
		t.Fatalf("refresh after geometry change = %d, want 75 (pixclock %d)", got, next.Pixclock)
	}
}

func TestParseDisplayGeometry(t *testing.T) {
	for _, tc := range []struct {
		text string
		w, h int
		ok   bool
	}{
		{"1920x1080", 1920, 1080, true},
		{"U:1280x720p60", 1280, 720, true},
		{"800x600-60", 800, 600, true},
		{"640x480", 640, 480, true},
		{"639x480", 0, 0, false},
		{"7680x4320", 7680, 4320, true},
		{"8000x4500", 0, 0, false},
		{"", 0, 0, false},
	} {
		w, h, ok := parseDisplayGeometry(tc.text)
		if w != tc.w || h != tc.h || ok != tc.ok {
			t.Errorf("parseDisplayGeometry(%q) = %d,%d,%v; want %d,%d,%v", tc.text, w, h, ok, tc.w, tc.h, tc.ok)
		}
	}
}

func TestMappingSizeRejectsOverflow(t *testing.T) {
	f := &fbDevice{lineBytes: int(^uint(0) >> 1), h: 2}
	if _, err := f.mappingSize(); err == nil {
		t.Fatal("expected mapping size overflow to be rejected")
	}
}

func TestInputGeometryRotationAndResize(t *testing.T) {
	d := desktop.New(800, 600, desktop.ModeDesktop, desktop.Host{})
	d.MouseX, d.MouseY = 100, 200
	s := newInputState(800, 600)

	s.setGeometry(800, 600, 90, d)
	if d.MouseX != 100 || d.MouseY != 200 || s.physX != 200 || s.physY != 499 {
		t.Fatalf("rotation moved logical cursor: desktop=%d,%d physical=%d,%d", d.MouseX, d.MouseY, s.physX, s.physY)
	}
	s.move(d, s.physX, s.physY)
	if d.MouseX != 100 || d.MouseY != 200 {
		t.Fatalf("rotated move = %d,%d, want 100,200", d.MouseX, d.MouseY)
	}

	s.setGeometry(1280, 720, 90, d)
	if s.physX != 200 || s.physY != 499 || d.MouseX != 220 || d.MouseY != 200 {
		t.Fatalf("resize lost physical cursor: desktop=%d,%d physical=%d,%d", d.MouseX, d.MouseY, s.physX, s.physY)
	}

	s.setGeometry(640, 480, 90, d)
	if s.physX != 200 || s.physY != 479 || d.MouseX != 0 || d.MouseY != 200 {
		t.Fatalf("clamped resize cursor: desktop=%d,%d physical=%d,%d", d.MouseX, d.MouseY, s.physX, s.physY)
	}
}
