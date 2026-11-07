package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"win95linux/internal/ui"
)

func welcomeDesktop(t *testing.T) *Desktop {
	t.Helper()
	old := prefsPath
	prefsPath = filepath.Join(t.TempDir(), "desktop.json")
	t.Cleanup(func() { prefsPath = old })
	return New(800, 600, ModeDesktop, Host{})
}

func TestWelcomeFirstBootShowsOnce(t *testing.T) {
	d := welcomeDesktop(t)
	if d.eulaAccepted {
		t.Fatal("fresh desktop should not have accepted the EULA")
	}
	d.MaybeShowWelcome()
	if d.modal == nil {
		t.Fatal("first boot should open the welcome modal")
	}
	a, ok := d.modal.App.(*welcomeApp)
	if !ok {
		t.Fatalf("modal is %T, want welcomeApp", d.modal.App)
	}
	if len(a.lines) == 0 {
		t.Fatal("license text was not laid out")
	}
	// Accept persists and closes the modal.
	a.accept()
	if !d.eulaAccepted {
		t.Fatal("accept should set the flag")
	}
	if d.modal != nil {
		t.Fatal("accept should close the welcome modal")
	}
	b, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("accept did not persist prefs: %v", err)
	}
	var p desktopPrefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if !p.EULAAccepted {
		t.Fatal("persisted prefs missing eula_accepted")
	}
	// A second boot with the same prefs must not show it again.
	d2 := New(800, 600, ModeDesktop, Host{})
	if !d2.eulaAccepted {
		t.Fatal("second boot should load the accepted flag")
	}
	d2.MaybeShowWelcome()
	if d2.modal != nil {
		t.Fatal("welcome must not reappear after acceptance")
	}
}

func TestWelcomeDeclineDoesNotPersist(t *testing.T) {
	d := welcomeDesktop(t)
	d.MaybeShowWelcome()
	a := d.modal.App.(*welcomeApp)
	a.decline()
	if d.eulaAccepted {
		t.Fatal("decline must not accept")
	}
	// Decline shows an informational dialog, not the welcome.
	if d.modal == nil {
		t.Fatal("decline should explain the consequence")
	}
	if _, ok := d.modal.App.(*welcomeApp); ok {
		t.Fatal("welcome should be closed after decline")
	}
	d2 := New(800, 600, ModeDesktop, Host{})
	if d2.eulaAccepted {
		t.Fatal("decline must not persist acceptance")
	}
	d2.MaybeShowWelcome()
	if d2.modal == nil {
		t.Fatal("welcome should reappear next boot after decline")
	}
}

func TestWelcomeInstallerModeSkips(t *testing.T) {
	old := prefsPath
	prefsPath = filepath.Join(t.TempDir(), "desktop.json")
	t.Cleanup(func() { prefsPath = old })
	d := New(800, 600, ModeInstaller, Host{})
	d.MaybeShowWelcome()
	for _, w := range d.windows {
		if _, ok := w.App.(*welcomeApp); ok {
			t.Fatal("installer mode must not show the desktop welcome")
		}
	}
}

func TestWelcomeScrollAndPaint(t *testing.T) {
	d := welcomeDesktop(t)
	d.MaybeShowWelcome()
	a := d.modal.App.(*welcomeApp)
	c := ui.NewCanvas(800, 600)
	d.Draw(c)
	if a.scroll.Max() == 0 {
		t.Fatal("license should be long enough to scroll")
	}
	before := a.scroll.Pos
	a.Key(d, d.modal, KeyEvent{Name: "PageDown"})
	if a.scroll.Pos <= before {
		t.Fatal("PageDown should scroll the license")
	}
	d.Draw(c)
	a.Key(d, d.modal, KeyEvent{Name: "End"})
	if a.scroll.Pos != a.scroll.Max() {
		t.Fatal("End should jump to the bottom")
	}
	d.Draw(c)
}
