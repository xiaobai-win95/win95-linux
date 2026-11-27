package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"win95linux/internal/snapshot"
)

func TestSnapshotDescriptionRoundTripAndUnsafeID(t *testing.T) {
	base := t.TempDir()
	if err := writeSnapshotDescription(base, "20260102T030405Z-abcdef", "before upgrade"); err != nil {
		t.Fatal(err)
	}
	got, err := readSnapshotDescription(base, "20260102T030405Z-abcdef")
	if err != nil || got != "before upgrade" {
		t.Fatalf("description=%q err=%v", got, err)
	}
	if _, err := snapshotDescriptionPath(base, "../escape"); err == nil {
		t.Fatal("unsafe snapshot ID accepted")
	}
	link := filepath.Join(base, ".description-link.txt")
	if err := os.Symlink(filepath.Join(base, "target"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSnapshotDescription(base, "link"); err == nil {
		t.Fatal("symlink description accepted")
	}
}

func TestSnapshotManagerRequiresExplicitSelection(t *testing.T) {
	item := snapshotManagerItem{snapshot: snapshot.Snapshot{ID: "snap-1", Root: "/root", Bytes: 1024}}
	a := &SnapshotManagerApp{sel: -1, items: []snapshotManagerItem{item}}
	if a.selected() != nil {
		t.Fatal("snapshot selected by default")
	}
	a.sel, a.chosen = 0, true
	if a.selected() == nil || a.selected().snapshot.ID != "snap-1" {
		t.Fatal("explicit snapshot selection failed")
	}
}

func TestSnapshotRestoreNeedsTwoWarnings(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	item := snapshotManagerItem{snapshot: snapshot.Snapshot{ID: "snap-1", CreatedAt: time.Now().UTC(), Root: "/root", Bytes: 2048}, description: "known good"}
	proceeded, cancelled := 0, 0
	confirmSnapshotRestore(d, item, "/root/work", "/var/lib/win95/snapshots", func() { proceeded++ }, func() { cancelled++ })
	first := d.modal
	if first == nil {
		t.Fatal("first restore warning missing")
	}
	text := strings.Join(first.App.(*dialogApp).lines, "\n")
	for _, want := range []string{"snap-1", "/root/work", "2.0 KB", "当前工作可能丢失"} {
		if !strings.Contains(text, want) {
			t.Fatalf("warning missing %q: %s", want, text)
		}
	}
	clickDesktopDialogButton(t, d, 1)
	if proceeded != 0 || cancelled != 1 {
		t.Fatalf("cancel flow proceeded=%d cancelled=%d", proceeded, cancelled)
	}
	confirmSnapshotRestore(d, item, "/root/work", "/var/lib/win95/snapshots", func() { proceeded++ }, func() { cancelled++ })
	clickDesktopDialogButton(t, d, 0)
	if d.modal == nil || !strings.Contains(d.modal.App.Title(), "最后确认") {
		t.Fatal("second restore warning missing")
	}
	clickDesktopDialogButton(t, d, 0)
	if proceeded != 1 {
		t.Fatal("restore ran without second confirmation")
	}
}

func TestValidateSnapshotRestoreRoot(t *testing.T) {
	base := t.TempDir()
	root := t.TempDir()
	got, err := validateSnapshotRestoreRoot(root, base, "snap-1")
	resolvedRoot, resolveErr := filepath.EvalSymlinks(root)
	if err != nil || resolveErr != nil || got != resolvedRoot {
		t.Fatalf("root=%q resolved=%q err=%v resolveErr=%v", got, resolvedRoot, err, resolveErr)
	}
	if _, err := validateSnapshotRestoreRoot("relative/path", base, "snap-1"); err == nil {
		t.Fatal("relative restore root accepted")
	}
	if _, err := validateSnapshotRestoreRoot(string(filepath.Separator), base, "snap-1"); err == nil {
		t.Fatal("filesystem root accepted as restore target")
	}
	if _, err := validateSnapshotRestoreRoot(base, base, "snap-1"); err == nil {
		t.Fatal("snapshot base accepted as restore root")
	}
	link := filepath.Join(t.TempDir(), "work-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSnapshotRestoreRoot(link, base, "snap-1"); err == nil {
		t.Fatal("symlink restore root accepted")
	}
}

func TestSnapshotManagerCreateListRestoreIntegration(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	root := t.TempDir()
	base := t.TempDir()
	work := filepath.Join(root, "work.txt")
	if err := os.WriteFile(work, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newSnapshotManagerApp(d)
	a.baseDir = base
	a.restoreFn = func(baseDir, id, restoreRoot string, progress func(snapshot.Progress)) error {
		if restoreRoot != a.items[0].snapshot.Root {
			t.Errorf("restore root %q was not resolved to %q", restoreRoot, a.items[0].snapshot.Root)
		}
		return snapshot.Restore(baseDir, id, restoreRoot, progress)
	}
	a.startCreate(root, "before edit")
	runDesktopPostedUntil(t, d, func() bool { return !a.busy && len(a.items) == 1 })
	if a.err != "" || a.items[0].description != "before edit" {
		t.Fatalf("snapshot list err=%q items=%+v", a.err, a.items)
	}
	if err := os.WriteFile(work, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.sel, a.chosen = 0, true
	a.startRestore(a.items[0], root)
	runDesktopPostedUntil(t, d, func() bool { return !a.busy && strings.Contains(a.status, "已恢复") })
	if a.err != "" {
		t.Fatalf("restore failed: %v", a.err)
	}
	data, err := os.ReadFile(work)
	if err != nil || string(data) != "original" {
		t.Fatalf("restored work=%q err=%v", data, err)
	}
}
