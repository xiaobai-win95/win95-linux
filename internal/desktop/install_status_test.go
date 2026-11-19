package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseInstallStatus(t *testing.T) {
	title, pct, msg, state := parseInstallStatus("title=Arch Linux 安装\npct=42\nmsg=正在下载（120 / 791 MB）\nstate=running\n")
	if title != "Arch Linux 安装" || pct != 42 || msg != "正在下载（120 / 791 MB）" || state != "running" {
		t.Fatalf("parsed title=%q pct=%d msg=%q state=%q", title, pct, msg, state)
	}
}

func TestParseInstallStatusClampsAndDefaults(t *testing.T) {
	_, pct, _, state := parseInstallStatus("pct=250\nstate=exploded\n")
	if pct != 100 {
		t.Fatalf("pct = %d, want 100", pct)
	}
	if state != "running" {
		t.Fatalf("state = %q, want running", state)
	}
	_, pct, _, _ = parseInstallStatus("pct=-3\n")
	if pct != 0 {
		t.Fatalf("negative pct = %d, want 0", pct)
	}
}

func TestTailTextLinesDropsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.log")
	content := "first\n\nsecond\r\nthird\n\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := tailTextLines(path, 2)
	if len(lines) != 2 || lines[0] != "second" || lines[1] != "third" {
		t.Fatalf("tail lines = %#v", lines)
	}
}
