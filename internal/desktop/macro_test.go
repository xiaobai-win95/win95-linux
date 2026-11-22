package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMacroRecordsSavesAndLoads(t *testing.T) {
	oldDir := macroDir
	macroDir = t.TempDir()
	defer func() { macroDir = oldDir }()

	d := New(1024, 768, ModeDesktop, Host{})
	a := NewMacroApp(d)
	a.win = d.Open(a)
	d.macro = a

	a.beginRecording(d)
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: 900, Y: 680})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: 900, Y: 680})
	d.Key(KeyEvent{Rune: 'a'})
	a.stopAll(d)

	if len(a.script.Events) != 3 {
		t.Fatalf("events = %d, want 3 (%+v)", len(a.script.Events), a.script.Events)
	}
	if a.script.Events[0].Type != "down" || a.script.Events[0].Button != 1 {
		t.Fatalf("first event = %+v", a.script.Events[0])
	}
	if last := a.script.Events[2]; last.Type != "key" || last.Rune != 'a' {
		t.Fatalf("key event = %+v", last)
	}

	a.script.Name = "测试脚本"
	a.saveScript(d)
	path := filepath.Join(macroDir, "测试脚本.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved script missing: %v", err)
	}
	var saved macroScript
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Events) != 3 || saved.Name != "测试脚本" {
		t.Fatalf("saved = %+v", saved)
	}
	if len(a.scripts) != 1 || a.sel != 0 {
		t.Fatalf("script list = %v sel=%d", a.scripts, a.sel)
	}

	a.script = macroScript{}
	a.loadPath(d, path)
	if len(a.script.Events) != 3 || a.script.Events[2].Rune != 'a' {
		t.Fatalf("loaded = %+v", a.script)
	}
}

func TestMacroPlaybackInjectsAndStops(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewMacroApp(d)
	a.win = d.Open(a)
	d.macro = a
	a.script.Events = []macroEvent{{T: 0, Type: "move", X: 321, Y: 222}}
	a.loops = 1

	a.startPlay(d)
	now := time.Now()
	a.step(d, now)
	d.RunPosted()
	if d.MouseX != 321 || d.MouseY != 222 {
		t.Fatalf("mouse = %d,%d, want 321,222", d.MouseX, d.MouseY)
	}
	if a.playing {
		t.Fatal("single-loop playback should finish")
	}

	a.script.Events = []macroEvent{{T: 0, Type: "move", X: 10, Y: 20}}
	a.playing = true
	a.playIdx = 0
	a.playLoop = 0
	a.playStart = now
	d.Key(KeyEvent{Name: "Esc"})
	if a.playing {
		t.Fatal("Esc should stop playback")
	}
}

func TestMacroSkipsItsOwnWindow(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewMacroApp(d)
	a.win = d.Open(a)
	d.macro = a
	a.beginRecording(d)
	cx, cy := a.win.X+a.win.W/2, a.win.Y+a.win.H/2
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: cx, Y: cy})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: cx, Y: cy})
	a.stopAll(d)
	if len(a.script.Events) != 0 {
		t.Fatalf("clicks on the recorder window must not be recorded: %+v", a.script.Events)
	}
}
