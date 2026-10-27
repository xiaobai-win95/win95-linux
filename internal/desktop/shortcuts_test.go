package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDesktop(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.desktop")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseShortcutLocalizedName(t *testing.T) {
	path := tempDesktop(t, `[Desktop Entry]
Type=Application
Name=Firefox
Name[zh_CN]=火狐浏览器
Exec=firefox %u
Icon=firefox
Categories=Network;WebBrowser;
Terminal=false
`)
	e, ok := parseShortcut(path)
	if !ok {
		t.Fatal("parseShortcut rejected a normal application entry")
	}
	if e.NameZh != "火狐浏览器" || e.Name != "Firefox" {
		t.Fatalf("names = %q / %q", e.Name, e.NameZh)
	}
	if e.Exec != "firefox %u" {
		t.Fatalf("Exec = %q", e.Exec)
	}
	if e.Terminal {
		t.Fatal("Terminal parsed as true")
	}
}

func TestParseShortcutRejectsHiddenEntries(t *testing.T) {
	for _, body := range []string{
		"[Desktop Entry]\nType=Application\nName=X\nExec=x\nNoDisplay=true\n",
		"[Desktop Entry]\nType=Application\nName=X\nExec=x\nHidden=true\n",
		"[Desktop Entry]\nType=Link\nName=X\nExec=x\n",
		"[Desktop Entry]\nName=X\n",
	} {
		if _, ok := parseShortcut(tempDesktop(t, body)); ok {
			t.Fatalf("accepted hidden or incomplete entry:\n%s", body)
		}
	}
}

func TestParseShortcutIgnoresOtherGroups(t *testing.T) {
	path := tempDesktop(t, `[Desktop Action new]
Name=Wrong
Exec=wrong

[Desktop Entry]
Name=Right
Exec=right %F
`)
	e, ok := parseShortcut(path)
	if !ok {
		t.Fatal("entry not parsed")
	}
	if e.Name != "Right" || e.Exec != "right %F" {
		t.Fatalf("got %q / %q", e.Name, e.Exec)
	}
}

func TestCleanExecDropsFieldCodes(t *testing.T) {
	cases := map[string]string{
		"firefox %u":                    "firefox",
		"/usr/bin/app %F --flag":        "/usr/bin/app --flag",
		"xfce4-terminal --drop-down %U": "xfce4-terminal --drop-down",
		"app file%f":                    "app file",
		"app":                           "app",
	}
	for in, want := range cases {
		if got := cleanExec(in); got != want {
			t.Fatalf("cleanExec(%q) = %q, want %q", in, got, want)
		}
	}
}
