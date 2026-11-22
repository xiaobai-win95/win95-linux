package desktop

import (
	"testing"
	"time"
)

func TestDoomMapEnclosed(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewDoomApp(d)
	if a.gw != 20 || a.gh != 20 {
		t.Fatalf("map = %d×%d", a.gw, a.gh)
	}
	for x := 0; x < a.gw; x++ {
		if a.cell(x, 0) == 0 || a.cell(x, a.gh-1) == 0 {
			t.Fatalf("map border open at column %d", x)
		}
	}
	for y := 0; y < a.gh; y++ {
		if a.cell(0, y) == 0 || a.cell(a.gw-1, y) == 0 {
			t.Fatalf("map border open at row %d", y)
		}
	}
	if a.blocked(a.px, a.py) {
		t.Fatal("spawn point is inside a wall")
	}
}

func TestDoomRenderProducesTexturedFrame(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewDoomApp(d)
	if len(a.frame) != doomRW*doomRH {
		t.Fatalf("frame = %d", len(a.frame))
	}
	colors := map[uint32]bool{}
	for _, p := range a.frame {
		colors[p] = true
	}
	if len(colors) < 200 {
		t.Fatalf("flat frame, only %d colors", len(colors))
	}
	before := a.frame[100*doomRW+160]
	a.ang = 1.2
	a.render()
	if a.frame[100*doomRW+160] == before {
		t.Fatal("turning should change the view")
	}
}

func TestDoomMovementAndFrameLock(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewDoomApp(d)
	w := d.Open(a)
	a.client = w.ClientRect()
	start := a.px
	now := time.Now()
	for i := 0; i < 40; i++ {
		now = now.Add(33 * time.Millisecond)
		a.keys.fwd = true
		a.keys.until = now.Add(time.Second)
		a.Animate(d, w, now)
	}
	if a.px <= start {
		t.Fatalf("player did not move: px %.2f -> %.2f", start, a.px)
	}
	if !a.blocked(a.px+0.25, a.py) {
		t.Fatalf("player should stop at the wall, px=%.2f", a.px)
	}
	renders := a.renders
	a.Animate(d, w, now.Add(5*time.Millisecond))
	if a.renders != renders {
		t.Fatal("frames must be locked to 30 FPS")
	}
}

func TestDoomKeysToggleMapAndClose(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewDoomApp(d)
	w := d.Open(a)
	a.win = w
	a.Key(d, w, KeyEvent{Name: "M"})
	if a.showMap {
		t.Fatal("M should hide the minimap")
	}
	a.Key(d, w, KeyEvent{Name: "W"})
	if !a.keys.fwd {
		t.Fatal("W should set forward")
	}
	a.Key(d, w, KeyEvent{Name: "Esc"})
	for _, win := range d.windows {
		if win == w {
			t.Fatal("Esc should close the game window")
		}
	}
}
