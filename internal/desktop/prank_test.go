package desktop

import (
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestPrankWarningRequiresConfirmation(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	if d.prank == nil || d.prank.stage != prankStageWarning {
		t.Fatalf("expected warning stage, got %+v", d.prank)
	}
	// Time alone must never start the demo.
	d.prank.animate(d, time.Now().Add(time.Hour))
	if d.prank.stage != prankStageWarning {
		t.Fatalf("warning stage advanced without confirmation")
	}
	d.Key(KeyEvent{Name: "Esc"})
	if d.prank != nil {
		t.Fatalf("Esc should cancel the demo")
	}
	if d.modal == nil {
		t.Fatalf("cancelling should show a confirmation dialog")
	}
}

func TestPrankMouseChoosesGentleMode(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	c := ui.NewCanvas(1024, 768)
	d.openPrankDemo()
	d.Draw(c)
	r := d.prank.gentleR
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	if d.prank == nil || d.prank.stage != prankStageFormat {
		t.Fatalf("gentle button should start the format stage")
	}
	if !d.prank.gentle {
		t.Fatalf("gentle mode not recorded")
	}
}

func TestPrankStageTransitions(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)

	transitions := []struct {
		from, to int
		dur      time.Duration
	}{
		{prankStageFormat, prankStageDance, prankFormatDur},
		{prankStageDance, prankStageStripes, prankDanceDur},
		{prankStageStripes, prankStageShapes, prankStripesDur},
		{prankStageShapes, prankStageMessage, prankShapesDur},
		{prankStageMessage, prankStageVortex, prankMessageDur},
		{prankStageVortex, prankStageChaos, prankVortexDur},
		{prankStageChaos, prankStageBSOD, prankChaosDur},
	}
	for _, tr := range transitions {
		if d.prank == nil || d.prank.stage != tr.from {
			t.Fatalf("expected stage %d, got %+v", tr.from, d.prank)
		}
		p := d.prank
		p.stageAt = time.Now()
		p.animate(d, p.stageAt.Add(tr.dur))
		if d.prank == nil || d.prank.stage != tr.to {
			t.Fatalf("stage %d did not advance to %d: %+v", tr.from, tr.to, d.prank)
		}
	}
	if len(d.prank.items) == 0 {
		t.Fatalf("vortex should have gathered desktop icons and windows")
	}

	p := d.prank
	p.stageAt = time.Now()
	p.animate(d, p.stageAt.Add(prankBSODDur))
	if d.prank != nil {
		t.Fatalf("fake BSOD should finish the demo")
	}
	if d.modal == nil {
		t.Fatalf("finishing should show the safety recap dialog")
	}
}

func TestPrankDrawsEveryStage(t *testing.T) {
	for _, size := range [][2]int{{1024, 768}, {800, 600}} {
		c := ui.NewCanvas(size[0], size[1])
		d := New(size[0], size[1], ModeDesktop, Host{})
		d.openPrankDemo()
		d.Draw(c)
		d.prank.begin(false)
		stages := []int{
			prankStageFormat,
			prankStageDance,
			prankStageStripes,
			prankStageShapes,
			prankStageMessage,
			prankStageVortex,
			prankStageChaos,
			prankStageBSOD,
		}
		for _, stage := range stages {
			d.prank.enter(d, stage, time.Now())
			d.Draw(c)
		}
	}
}

func TestPrankSnapshotExcludesOverlay(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	d.prank.enter(d, prankStageDance, time.Now())
	c := ui.NewCanvas(800, 600)
	d.Draw(c)
	if d.prank.snap == nil {
		t.Fatal("dance stage should capture the desktop")
	}
	if d.prank.snap.W != 800 || d.prank.snap.H != 600 {
		t.Fatalf("snapshot size = %d×%d", d.prank.snap.W, d.prank.snap.H)
	}
	// The snapshot must be the normal desktop, not the prank's own yellow banner.
	if d.prank.snap.Pix[0] == 0xFFD000 {
		t.Fatal("snapshot contains the prank banner")
	}
}

// Once confirmed the demo cannot be aborted mid-run; only the warning screen
// honours Esc. Keys during a running stage reach the real desktop instead.
func TestPrankNoMidRunExit(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	for _, ev := range []KeyEvent{
		{Name: "Esc"},
		{Name: "F4", Alt: true},
		{Name: "Esc", Ctrl: true},
	} {
		d.Key(ev)
		if d.prank == nil || d.prank.stage != prankStageFormat {
			t.Fatalf("running demo must not exit on %+v", ev)
		}
	}
}

// While the demo runs, mouse input still reaches the real desktop: clicking the
// Start button opens the Start menu above the overlay, and input briefly reveals
// the real scene before the overlay re-covers it.
func TestPrankInputReachesDesktop(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	top := d.taskbarTop()
	x, y := 4+(startBtnW-2)/2, top+4+13
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: x, Y: y})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: x, Y: y})
	if !d.startOpen {
		t.Fatalf("Start menu should open under the overlay")
	}
	if d.prank == nil {
		t.Fatalf("opening a menu must not stop the demo")
	}
	if !time.Now().Before(d.prank.revealUntil) {
		t.Fatalf("input should briefly reveal the real desktop")
	}
}

func TestPrankInvertGating(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	p := d.prank
	p.runAt = time.Now()
	p.stage = prankStageDance
	if !p.invertActive(p.runAt) {
		t.Fatal("normal mode should invert on even seconds")
	}
	if p.invertActive(p.runAt.Add(time.Second)) {
		t.Fatal("normal mode should not invert on odd seconds")
	}
	if !p.invertActive(p.runAt.Add(2 * time.Second)) {
		t.Fatal("normal mode should invert again on even seconds")
	}
	p.stage = prankStageFormat
	if p.invertActive(p.runAt) {
		t.Fatal("format stage must not invert")
	}
	p.gentle = true
	p.stage = prankStageDance
	if p.invertActive(p.runAt) {
		t.Fatal("gentle mode must never invert")
	}
}

func TestPrankPopupsSpawnAndExpire(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	p := d.prank
	now := time.Now()
	p.stage = prankStageDance
	p.nextPopupAt = now.Add(-time.Second)
	p.updatePopups(d, now)
	if len(p.popups) == 0 {
		t.Fatal("dance stage should spawn fake popups")
	}
	// An expired popup is removed and nothing new spawns while the timer is set
	// far in the future.
	p.popups = []prankPopup{{x: 10, y: 10, w: 200, h: 100, born: now.Add(-2 * time.Second), life: time.Second}}
	p.nextPopupAt = now.Add(10 * time.Second)
	p.updatePopups(d, now)
	if len(p.popups) != 0 {
		t.Fatalf("expired popups should close themselves, got %d", len(p.popups))
	}
}

func TestPrankPopupsDraw(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openPrankDemo()
	d.prank.begin(false)
	p := d.prank
	now := time.Now()
	p.popups = []prankPopup{
		{x: 40, y: 40, w: 280, h: 170, born: now, life: time.Second, terminal: true, seed: 2},
		{x: 400, y: 200, w: 320, h: 140, born: now, life: time.Second, terminal: false, seed: 3},
	}
	c := ui.NewCanvas(1024, 768)
	p.drawPopups(c, now)
	p.drawPopups(c, now.Add(400*time.Millisecond))
}
