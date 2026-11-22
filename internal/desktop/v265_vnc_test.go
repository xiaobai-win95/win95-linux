package desktop

import (
	"testing"

	"win95linux/internal/ui"
)

func TestKeysymEventMapping(t *testing.T) {
	cases := []struct {
		ks    uint32
		shift bool
		want  KeyEvent
	}{
		{0x61, true, KeyEvent{Rune: 'A', Shift: true}},
		{0x61, false, KeyEvent{Rune: 'a'}},
		{0xff0d, false, KeyEvent{Name: "Enter"}},
		{0xff51, false, KeyEvent{Name: "Left"}},
		{0xffc1, false, KeyEvent{Name: "F4"}},
		{0x20, false, KeyEvent{Rune: ' ', Name: "Space"}},
		{0x010000e9, false, KeyEvent{Rune: 'é'}},
	}
	for _, tc := range cases {
		got, ok := keysymEvent(tc.ks, tc.shift, false, false)
		if !ok || got != tc.want {
			t.Errorf("keysym %#x shift=%v = %+v ok=%v, want %+v", tc.ks, tc.shift, got, ok, tc.want)
		}
	}
	if _, ok := keysymEvent(0xffe1, false, false, false); ok {
		t.Error("modifier keys must not produce events")
	}
}

func TestVNCPointerEvents(t *testing.T) {
	evs := vncPointerEvents(800, 600, 999, -5, 1, 0)
	if len(evs) != 2 {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0].Kind != MouseMove || evs[0].X != 799 || evs[0].Y != 0 {
		t.Fatalf("move = %+v", evs[0])
	}
	if evs[1].Kind != MouseDown || evs[1].Button != 1 {
		t.Fatalf("down = %+v", evs[1])
	}
	evs = vncPointerEvents(800, 600, 10, 10, 0, 1)
	if len(evs) != 2 || evs[1].Kind != MouseUp {
		t.Fatalf("release = %+v", evs)
	}
	evs = vncPointerEvents(800, 600, 10, 10, 8, 0)
	if len(evs) != 2 || evs[1].Kind != MouseWheel || evs[1].Wheel != 1 {
		t.Fatalf("wheel = %+v", evs)
	}
}

func TestVNCAppPaint(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &VNCApp{d: d}
	c := ui.NewCanvas(620, 470)
	a.Paint(d, nil, c, ui.Rect{0, 0, 620, 470})
	if a.btnPower.W == 0 || a.btnCopy.W == 0 {
		t.Fatalf("buttons = %+v %+v", a.btnPower, a.btnCopy)
	}
	if a.Title() != "系统级 VNC 服务器" {
		t.Fatalf("title = %q", a.Title())
	}
}
