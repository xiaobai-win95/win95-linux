package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestSession(t *testing.T) *psSession {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "win95-pshell-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := newPSSession()
	s.cwd = dir
	return s
}

func runPS(t *testing.T, s *psSession, line string) string {
	t.Helper()
	out, _, _ := s.Execute(line)
	var b strings.Builder
	for _, l := range out {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestPShellEngineFilesAndPipelines(t *testing.T) {
	s := newTestSession(t)

	if got := strings.TrimSpace(runPS(t, s, `Write-Output "你好"`)); got != "你好" {
		t.Fatalf("Write-Output = %q", got)
	}
	got := runPS(t, s, `$name = "世界"; Write-Output "你好，$name"`)
	if !strings.Contains(got, "你好，世界") {
		t.Fatalf("variable expansion: %q", got)
	}
	got = runPS(t, s, `$name = "世界"; Write-Output '$name'; Write-Output "$name"`)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 || lines[0] != "$name" || lines[1] != "世界" {
		t.Fatalf("quote handling: %#v", lines)
	}

	if out := runPS(t, s, `New-Item note.txt`); !strings.Contains(out, "已创建文件") {
		t.Fatalf("New-Item: %q", out)
	}
	if _, err := os.Stat(filepath.Join(s.cwd, "note.txt")); err != nil {
		t.Fatalf("note.txt not created: %v", err)
	}
	runPS(t, s, `Set-Content note.txt "第一行"`)
	runPS(t, s, `Add-Content note.txt "第二行"`)
	content := runPS(t, s, `Get-Content note.txt`)
	if !strings.Contains(content, "第一行") || !strings.Contains(content, "第二行") {
		t.Fatalf("Get-Content: %q", content)
	}
	if out := runPS(t, s, `Get-Content note.txt | Select-String 第二`); !strings.Contains(out, "第二行") {
		t.Fatalf("Select-String: %q", out)
	}

	runPS(t, s, `mkdir 子目录`)
	if st, err := os.Stat(filepath.Join(s.cwd, "子目录")); err != nil || !st.IsDir() {
		t.Fatalf("mkdir failed: %v", err)
	}
	list := runPS(t, s, `Get-ChildItem`)
	if !strings.Contains(list, "note.txt") || !strings.Contains(list, "子目录") {
		t.Fatalf("Get-ChildItem: %q", list)
	}

	runPS(t, s, `Copy-Item note.txt copy.txt`)
	runPS(t, s, `Move-Item copy.txt moved.txt`)
	runPS(t, s, `Rename-Item moved.txt final.txt`)
	if out := strings.TrimSpace(runPS(t, s, `Test-Path final.txt`)); out != "True" {
		t.Fatalf("Test-Path = %q", out)
	}
	if out := runPS(t, s, `Get-ChildItem | Measure-Object`); !strings.Contains(out, "Count") {
		t.Fatalf("Measure-Object: %q", out)
	}

	// -WhatIf must never delete anything.
	out := runPS(t, s, `Remove-Item final.txt -WhatIf`)
	if !strings.Contains(out, "WhatIf") {
		t.Fatalf("WhatIf output: %q", out)
	}
	if _, err := os.Stat(filepath.Join(s.cwd, "final.txt")); err != nil {
		t.Fatalf("WhatIf deleted the file: %v", err)
	}
	runPS(t, s, `Remove-Item final.txt`)
	if _, err := os.Stat(filepath.Join(s.cwd, "final.txt")); !os.IsNotExist(err) {
		t.Fatalf("Remove-Item did not delete: %v", err)
	}

	// Protected system locations are refused.
	out = runPS(t, s, `Remove-Item C:\etc`)
	if !strings.Contains(out, "拒绝") && !strings.Contains(out, "受保护") {
		t.Fatalf("protected path was not refused: %q", out)
	}
	if _, err := os.Stat("/etc"); err != nil {
		t.Fatalf("/etc disappeared: %v", err)
	}
}

func TestPShellEngineProcessAndExternal(t *testing.T) {
	s := newTestSession(t)
	out := runPS(t, s, `Get-Process | Select-Object -First 3`)
	if strings.Count(out, "\n") < 3 {
		t.Fatalf("Get-Process output too short: %q", out)
	}
	out = runPS(t, s, `uname -s`)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("external command produced no output")
	}
	out = runPS(t, s, `Get-ComputerInfo`)
	if !strings.Contains(out, "OsName") || !strings.Contains(out, "26.2") {
		t.Fatalf("Get-ComputerInfo: %q", out)
	}
}

func TestPShellConsoleTyping(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{Info: "test"})
	app := NewPShellApp(d)
	d.Open(app)
	for _, r := range "Get-Date" {
		app.Key(d, app.win, KeyEvent{Rune: r})
	}
	if string(app.input) != "Get-Date" {
		t.Fatalf("input = %q", string(app.input))
	}
	app.Key(d, app.win, KeyEvent{Name: "Enter"})
	if !app.busy {
		t.Fatalf("command should be running")
	}
	deadline := time.Now().Add(3 * time.Second)
	for app.busy && time.Now().Before(deadline) {
		d.RunPosted()
		time.Sleep(10 * time.Millisecond)
	}
	if app.busy {
		t.Fatalf("command did not finish")
	}
	found := false
	for _, l := range app.lines {
		if strings.Contains(l.text, "年") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Get-Date output missing")
	}
}

func TestPShellISEExecute(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{Info: "test"})
	app := NewPShellISEApp(d)
	d.Open(app)
	app.sess.cwd = t.TempDir()
	app.SetScript("", "Write-Output \"ISE 测试\"\nGet-Date")
	app.run()
	if !app.busy {
		t.Fatalf("ISE script should be running")
	}
	deadline := time.Now().Add(3 * time.Second)
	for app.busy && time.Now().Before(deadline) {
		d.RunPosted()
		time.Sleep(10 * time.Millisecond)
	}
	if app.busy {
		t.Fatalf("ISE script did not finish")
	}
	joined := ""
	for _, l := range app.out {
		joined += l.text + "\n"
	}
	if !strings.Contains(joined, "ISE 测试") || !strings.Contains(joined, "年") {
		t.Fatalf("ISE output: %q", joined)
	}
}
