package desktop

import (
	"testing"

	"win95linux/internal/ui"
)

type keyCaptureApp struct {
	Base
	keys []KeyEvent
}

func (a *keyCaptureApp) Title() string                                { return "capture" }
func (a *keyCaptureApp) Icon() ui.IconKind                            { return ui.IcoKeyboard }
func (a *keyCaptureApp) Paint(*Desktop, *Window, *ui.Canvas, ui.Rect) {}
func (a *keyCaptureApp) Key(_ *Desktop, _ *Window, ev KeyEvent) bool {
	a.keys = append(a.keys, ev)
	return true
}

func softKeyCenter(d *Desktop, label string) (int, int, bool) {
	lay := d.softKeyboardLayout()
	for _, kg := range lay.keys {
		if d.softKeyLabel(kg.key) == label {
			return kg.rect.X + kg.rect.W/2, kg.rect.Y + kg.rect.H/2, true
		}
	}
	return 0, 0, false
}

func clickSoftKey(d *Desktop, t *testing.T, label string) {
	t.Helper()
	x, y, ok := softKeyCenter(d, label)
	if !ok {
		t.Fatalf("soft key %q not found", label)
	}
	d.Mouse(MouseEvent{Kind: MouseDown, X: x, Y: y, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: x, Y: y, Button: 1})
}

func TestSoftKeyboardTypingAndModifiers(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	app := &keyCaptureApp{}
	d.Open(app)
	d.toggleSoftKeyboard()
	if d.sk == nil {
		t.Fatal("soft keyboard did not open")
	}
	clickSoftKey(d, t, "a")
	if len(app.keys) != 1 || app.keys[0].Rune != 'a' || app.keys[0].Name != "A" {
		t.Fatalf("unexpected key: %+v", app.keys)
	}
	clickSoftKey(d, t, "Shift")
	if !d.sk.shift {
		t.Fatal("shift was not latched")
	}
	clickSoftKey(d, t, "A")
	if len(app.keys) != 2 || app.keys[1].Rune != 'A' || !app.keys[1].Shift {
		t.Fatalf("shift did not apply: %+v", app.keys[1])
	}
	if d.sk.shift {
		t.Fatal("shift should clear after one key")
	}
	clickSoftKey(d, t, "Ctrl")
	clickSoftKey(d, t, "s")
	last := app.keys[len(app.keys)-1]
	if !last.Ctrl || last.Name != "S" {
		t.Fatalf("ctrl shortcut not produced: %+v", last)
	}
	if d.sk.ctrl {
		t.Fatal("ctrl should clear after one key")
	}
}

func TestSoftKeyboardToggleHotkey(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	d.Key(KeyEvent{Name: "K", Rune: 'k', Ctrl: true, Alt: true})
	if d.sk == nil {
		t.Fatal("Ctrl+Alt+K did not open the keyboard")
	}
	d.Key(KeyEvent{Name: "K", Rune: 'k', Ctrl: true, Alt: true})
	if d.sk != nil {
		t.Fatal("Ctrl+Alt+K did not close the keyboard")
	}
}
