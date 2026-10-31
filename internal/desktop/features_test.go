package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func testDesktop(t *testing.T) *Desktop {
	t.Helper()
	old := prefsPath
	prefsPath = filepath.Join(t.TempDir(), "desktop.json")
	t.Cleanup(func() { prefsPath = old })
	return New(800, 600, ModeDesktop, Host{})
}

func TestRunURLRecognition(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://example.com/a", "https://example.com/a", true},
		{"HTTP://example.com", "HTTP://example.com", true},
		{"file:///root/x.html", "file:///root/x.html", true},
		{"example.com", "https://example.com", true},
		{"www.example.org/path", "https://www.example.org/path", true},
		{"example.cn", "https://example.cn", true},
		{"example.unknown", "", false},
		{"not a url", "", false},
		{"localhost", "", false},
	}
	for _, c := range cases {
		got, ok := runURL(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("runURL(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRunCommandAliasesAndUnknown(t *testing.T) {
	d := testDesktop(t)
	before := len(d.windows)
	if !d.runCommand("calc") {
		t.Fatal("calc should be recognized")
	}
	if len(d.windows) != before+1 {
		t.Fatalf("calc opened %d windows, want %d", len(d.windows)-before, 1)
	}
	if _, ok := d.windows[len(d.windows)-1].App.(*CalcApp); !ok {
		t.Fatalf("calc opened %T", d.windows[len(d.windows)-1].App)
	}
	if d.runCommand("definitely-not-a-command") {
		t.Fatal("unknown command should not resolve")
	}
}

func TestRunCommandOpensExistingFile(t *testing.T) {
	d := testDesktop(t)
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !d.runCommand(path) {
		t.Fatalf("existing file %q should resolve", path)
	}
	if _, ok := d.windows[len(d.windows)-1].App.(*NotepadApp); !ok {
		t.Fatalf("opened %T, want notepad", d.windows[len(d.windows)-1].App)
	}
}

func TestRunHistoryDedupesAndPersists(t *testing.T) {
	d := testDesktop(t)
	for i := 0; i < 12; i++ {
		d.addRunHistory(string(rune('a' + i)))
	}
	if len(d.runHistory) != 10 {
		t.Fatalf("history length = %d, want 10", len(d.runHistory))
	}
	d.addRunHistory("a")
	if d.runHistory[0] != "a" {
		t.Fatalf("most recent history = %q, want a", d.runHistory[0])
	}
	count := 0
	for _, cmd := range d.runHistory {
		if cmd == "a" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("history contains %d copies of a", count)
	}
	b, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	var p desktopPrefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.RunHistory) != 10 || p.RunHistory[0] != "a" {
		t.Fatalf("persisted history = %v", p.RunHistory)
	}
}

func TestDisplayApplyAndRestore(t *testing.T) {
	d := testDesktop(t)
	d.background, d.saverStyle, d.saverWait = 1, 1, 3*time.Minute
	a := &DisplayApp{
		d: d, bg: 4, style: 2, wait: 7,
		origBg: 1, origStyle: 1, origWait: 3,
	}
	a.apply()
	if d.background != 4 || d.saverStyle != 2 || d.saverWait != 7*time.Minute {
		t.Fatalf("apply produced background=%d style=%d wait=%v", d.background, d.saverStyle, d.saverWait)
	}
	b, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("apply did not persist prefs: %v", err)
	}
	var p desktopPrefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Background != 4 || p.SaverStyle != 2 || p.SaverWait != 7 {
		t.Fatalf("persisted prefs = %+v", p)
	}
	a.restore()
	if d.background != 1 || d.saverStyle != 1 || d.saverWait != 3*time.Minute {
		t.Fatalf("restore produced background=%d style=%d wait=%v", d.background, d.saverStyle, d.saverWait)
	}
}

func TestFindFFindsFilesAndSkipsHidden(t *testing.T) {
	d := testDesktop(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "beta.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".secret.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "gamma.txt"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewFindApp(d)
	w := d.Open(a)
	a.win = w
	a.scope = []rune(root)
	a.query = []rune("*.txt")
	a.startSearch()
	waitFind(t, d, a)
	if len(a.results) != 2 {
		t.Fatalf("found %d results, want 2: %+v", len(a.results), a.results)
	}
	for _, r := range a.results {
		if r.name == ".secret.txt" {
			t.Fatal("hidden file was included")
		}
	}
	a.hidden = true
	a.startSearch()
	waitFind(t, d, a)
	if len(a.results) != 3 {
		t.Fatalf("with hidden found %d results, want 3", len(a.results))
	}
	a.recursive = false
	a.startSearch()
	waitFind(t, d, a)
	if len(a.results) != 2 {
		t.Fatalf("non-recursive found %d results, want 2", len(a.results))
	}
	a.Close()
}

func waitFind(t *testing.T, d *Desktop, a *FindApp) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.RunPosted()
		if !a.running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("find search timed out")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestFindRejectsMissingScope(t *testing.T) {
	d := testDesktop(t)
	a := NewFindApp(d)
	a.scope = []rune(filepath.Join(t.TempDir(), "missing"))
	a.startSearch()
	if a.running || !a.done {
		t.Fatalf("invalid scope left running=%v done=%v", a.running, a.done)
	}
	if a.status == "" {
		t.Fatal("invalid scope produced no status")
	}
}

func TestStarfieldMovesAndDraws(t *testing.T) {
	d := testDesktop(t)
	d.saverStyle = saverStarfield
	now := time.Now()
	d.startSaver(now)
	if len(d.saverStars) != 220 {
		t.Fatalf("stars = %d, want 220", len(d.saverStars))
	}
	z := d.saverStars[0].z
	d.stepSaver(now.Add(100 * time.Millisecond))
	if d.saverStars[0].z >= z {
		t.Fatal("star did not move toward the viewer")
	}
	c := ui.NewCanvas(800, 600)
	d.drawScreensaver(c)
	d.stopSaver(now.Add(time.Second))
	if d.saver || d.saverStars != nil {
		t.Fatal("saver did not reset")
	}
}

func TestBackgroundStylesDrawWithoutPanic(t *testing.T) {
	c := ui.NewCanvas(320, 240)
	for i := range backgroundStyles {
		c.FillRect(ui.Rect{0, 0, c.W, c.H}, 0)
		drawBackgroundRect(c, ui.Rect{0, 0, c.W, c.H}, i)
		nonZero := false
		for _, px := range c.Pix {
			if px != 0 {
				nonZero = true
				break
			}
		}
		if !nonZero {
			t.Errorf("background style %d drew nothing", i)
		}
	}
}
