package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"win95linux/internal/ui"
)

func TestFileManagerHiddenToggle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewFileManagerApp(d, dir)
	if len(a.entries) != 1 || a.entries[0].name != "visible.txt" {
		t.Fatalf("entries = %+v, want only visible.txt", a.entries)
	}
	a.showHidden = true
	a.reload(true)
	if len(a.entries) != 2 {
		t.Fatalf("entries = %d, want 2 after showing hidden files", len(a.entries))
	}
}

func TestFileManagerTabs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewFileManagerApp(d, dir1)
	a.path = dir2
	a.load()
	if a.tabs[0].path != dir2 {
		t.Fatalf("tab path = %q, want %q", a.tabs[0].path, dir2)
	}
	a.newTab()
	if len(a.tabs) != 2 || a.tab != 1 {
		t.Fatalf("tabs = %d active = %d, want 2/1", len(a.tabs), a.tab)
	}
	a.path = dir1
	a.load()
	a.switchTab(0)
	if a.path != dir2 {
		t.Fatalf("path after switch = %q, want %q", a.path, dir2)
	}
	a.closeTab()
	if len(a.tabs) != 1 || a.tab != 0 {
		t.Fatalf("tabs = %d active = %d, want 1/0", len(a.tabs), a.tab)
	}
}

func TestFileManagerCreateShortcut(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewFileManagerApp(d, dir)
	idx := -1
	for i, e := range a.entries {
		if e.name == "note.txt" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("note.txt not listed")
	}
	a.sel = idx
	a.createShortcut(d)
	link := filepath.Join(dir, "note - 快捷方式.txt")
	st, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("shortcut not created: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", link)
	}
	target, err := os.Readlink(link)
	if err != nil || target != src {
		t.Fatalf("readlink = %q, %v, want %q", target, err, src)
	}
}

func TestFileManagerClassify(t *testing.T) {
	cases := []struct {
		name string
		kind ui.IconKind
		typ  string
	}{
		{"main.go", ui.IcoText, "源代码"},
		{"config.yaml", ui.IcoText, "配置文件"},
		{"photo.jpg", ui.IcoImage, "JPEG 图片"},
		{"archive.zip", ui.IcoPackage, "压缩文件"},
		{"app.desktop", ui.IcoSetup, "应用程序快捷方式"},
	}
	for _, tc := range cases {
		kind, typ := fmClassify(tc.name)
		if kind != tc.kind || typ != tc.typ {
			t.Errorf("fmClassify(%q) = %v/%q, want %v/%q", tc.name, kind, typ, tc.kind, tc.typ)
		}
	}
}

func TestFileManagerVirtualEntriesNotReal(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewFileManagerApp(d, "")
	if len(a.entries) == 0 {
		t.Fatal("My Computer has no entries")
	}
	for i, e := range a.entries {
		if !e.virtual {
			continue
		}
		a.sel = i
		if _, ok := a.selectedReal(); ok {
			t.Errorf("virtual entry %q (%s) reported as a real file", e.name, e.path)
		}
		break
	}
}
