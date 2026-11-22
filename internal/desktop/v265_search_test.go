package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"win95linux/internal/ui"
)

type wsTestApp struct{ Base }

func (a *wsTestApp) Title() string                                { return "测试窗口" }
func (a *wsTestApp) Icon() ui.IconKind                            { return ui.IcoFile }
func (a *wsTestApp) Paint(*Desktop, *Window, *ui.Canvas, ui.Rect) {}
func (a *wsTestApp) DefaultSize() (int, int)                      { return 400, 300 }
func (a *wsTestApp) MinSize() (int, int)                          { return 200, 150 }
func (a *wsTestApp) Resizable() bool                              { return true }

func waitIndexReady(t *testing.T, idx *SearchIndex) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if _, ready, _ := idx.Stats(); ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("search index did not finish")
}

func TestSearchIndexFilenameAndContent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"alpha.txt":          "hello alpha",
		"BetaReport.md":      "contains UniqueNeedle inside",
		"sub/deep-alpha.log": "deep match here",
		".hidden/alpha.ini":  "secret",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := newSearchIndexWithRoots([]string{root})
	idx.ensureStarted()
	waitIndexReady(t, idx)

	got := idx.Search("alpha", 20)
	if len(got) != 3 {
		t.Fatalf("Search(alpha) got %d results, want 3 (index includes hidden files)", len(got))
	}

	scoped := idx.SearchIn("*alpha*", root, true, false, false, 20)
	if len(scoped) != 2 {
		t.Fatalf("SearchIn recursive without hidden = %d, want 2", len(scoped))
	}
	direct := idx.SearchIn("*alpha*", root, false, false, false, 20)
	if len(direct) != 1 || direct[0].name != "alpha.txt" {
		t.Fatalf("SearchIn non-recursive = %+v, want only alpha.txt", direct)
	}
	withHidden := idx.SearchIn("*alpha*", root, true, true, false, 20)
	if len(withHidden) != 3 {
		t.Fatalf("SearchIn hidden = %d, want 3", len(withHidden))
	}
	filesOnly := idx.SearchIn("*", root, true, false, true, 50)
	for _, r := range filesOnly {
		if r.dir {
			t.Fatalf("filesOnly returned directory %s", r.path)
		}
	}

	content := idx.ContentSearch(context.Background(), "uniqueneedle", 10)
	if len(content) != 1 || filepath.Base(content[0].path) != "BetaReport.md" {
		t.Fatalf("ContentSearch = %+v, want BetaReport.md", content)
	}
	if !idx.Covers(root) {
		t.Fatal("index should cover its root")
	}
	if idx.Covers("/definitely/not/indexed") {
		t.Fatal("index must not cover arbitrary paths")
	}
}

func TestWorkspacesTilingAndSpotlightHotkey(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	w1 := d.Open(&wsTestApp{})
	if w1.Workspace != 0 {
		t.Fatalf("first window workspace = %d, want 0", w1.Workspace)
	}
	d.tileActive(tileRight)
	if !w1.tiled {
		t.Fatal("window should be tiled")
	}
	area := d.workspace()
	half := (area.W - 12) / 2
	if w1.W != half || w1.X <= area.W/2 {
		t.Fatalf("tileRight bounds = %d,%d,%d, want right half width %d", w1.X, w1.Y, w1.W, half)
	}
	d.untileActive()
	if w1.tiled || w1.W == half {
		t.Fatal("untile should restore floating bounds")
	}

	d.Key(KeyEvent{Alt: true, Name: "2"})
	if d.ws != 1 {
		t.Fatalf("workspace after Alt+2 = %d, want 1", d.ws)
	}
	if d.active != nil {
		t.Fatal("empty workspace should have no active window")
	}
	w2 := d.Open(&wsTestApp{})
	if w2.Workspace != 1 {
		t.Fatalf("second window workspace = %d, want 1", w2.Workspace)
	}
	w2.X, w2.Y = 600, 400
	if d.windowAt(w1.X+10, w1.Y+10) != nil {
		t.Fatal("window from another workspace must not be hit-testable")
	}
	if got := len(d.taskbarButtons()); got != 1 {
		t.Fatalf("taskbar buttons on workspace 2 = %d, want 1", got)
	}
	d.Key(KeyEvent{Alt: true, Name: "1"})
	if d.ws != 0 || d.active != w1 {
		t.Fatalf("Alt+1 did not return to window 1 (ws=%d active=%p)", d.ws, d.active)
	}

	d.Key(KeyEvent{Ctrl: true, Name: "Space", Rune: ' '})
	if d.spotlight == nil || !d.spotlight.active {
		t.Fatal("Ctrl+Space should open the spotlight overlay")
	}
	d.Key(KeyEvent{Name: "Esc"})
	if d.spotlight.active {
		t.Fatal("Esc should close the spotlight overlay")
	}

	d.Key(KeyEvent{Ctrl: true, Name: "F", Rune: 'f'})
	found := false
	for _, w := range d.windows {
		if _, ok := w.App.(*FindApp); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("Ctrl+F should open the Find window")
	}
}
