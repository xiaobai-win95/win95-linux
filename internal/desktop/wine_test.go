package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWineAppDetectsMissingAndInstalled(t *testing.T) {
	oldBin, oldPrefix := wineBinary, winePrefixDir
	defer func() { wineBinary, winePrefixDir = oldBin, oldPrefix }()

	wineBinary = "win95-no-such-wine"
	winePrefixDir = filepath.Join(t.TempDir(), ".wine")
	d := New(1024, 768, ModeDesktop, Host{})
	a := d.openWine()
	if !strings.Contains(a.status, "尚未安装") {
		t.Fatalf("missing Wine status = %q", a.status)
	}

	script := filepath.Join(t.TempDir(), "wine")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho wine-11.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wineBinary = script
	a.refresh()
	if !strings.Contains(a.status, "wine-11.0") {
		t.Fatalf("installed Wine status = %q", a.status)
	}
	if !wineAvailable() {
		t.Fatal("wineAvailable did not find the fake binary")
	}
}

func TestWineProgramRequiresInstall(t *testing.T) {
	oldBin := wineBinary
	defer func() { wineBinary = oldBin }()
	wineBinary = "win95-no-such-wine"
	d := New(1024, 768, ModeDesktop, Host{})
	d.openWineProgram("/root/示例.exe")
	if d.modal == nil {
		t.Fatal("running an exe without Wine did not show a warning")
	}
	dlg, ok := d.modal.App.(*dialogApp)
	if !ok || dlg.title != "Wine 兼容层" {
		t.Fatalf("unexpected warning dialog %T", d.modal.App)
	}
	found := false
	for _, w := range d.windows {
		if _, ok := w.App.(*WineApp); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("Wine window did not open after the warning")
	}
}

func TestOpenFileDefaultRoutesExeToWine(t *testing.T) {
	oldBin := wineBinary
	defer func() { wineBinary = oldBin }()
	wineBinary = "win95-no-such-wine"
	d := New(1024, 768, ModeDesktop, Host{})
	d.openFileDefault("/root/setup.exe")
	if d.modal == nil {
		t.Fatal(".exe did not route to the Wine install warning")
	}
}
