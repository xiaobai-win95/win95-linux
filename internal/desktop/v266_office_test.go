package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOfficeInstallState(t *testing.T) {
	data := "title=Office\nstate=running\nrun_id=run-1\npid=123\nstarted=1700000000\npackages=libreoffice-writer fcitx5\nmsg=installing\n"
	st, ok := parseOfficeInstallState(data)
	if !ok || st.RunID != "run-1" || st.PID != 123 || st.Packages != "libreoffice-writer fcitx5" {
		t.Fatalf("state = %+v, %v", st, ok)
	}
	if st.Started.Unix() != 1700000000 {
		t.Fatalf("started = %v", st.Started)
	}
}

func TestInstallStatusUsesJobSpecificFiles(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "office.status")
	logPath := filepath.Join(dir, "office.log")
	if err := os.WriteFile(statusPath, []byte("title=Office\npct=42\nmsg=working\nstate=running\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(800, 600, ModeDesktop, Host{})
	a := newInstallStatusAppAt(d, "fallback", statusPath, logPath)
	if a.title != "Office" || a.pct != 42 || a.msg != "working" || len(a.log) != 2 {
		t.Fatalf("status = %+v", a)
	}
}

func TestOfficeInstalledSelectionNeedsNoReinstall(t *testing.T) {
	installed := fakeOfficeInstalled("core", "writer", "calc", "impress", "draw", "math", "pdf", "ime", "langs")
	sel := officeSelectionDefaults(installed)
	if got := officeMissingPackages(sel, installed); len(got) != 0 {
		t.Fatalf("installed Office selection requested packages again: %v", got)
	}
}
