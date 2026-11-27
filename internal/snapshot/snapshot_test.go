package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testTree(t *testing.T) (root, base string) {
	t.Helper()
	parent := t.TempDir()
	root = filepath.Join(parent, "root")
	base = filepath.Join(parent, "snapshots")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, base
}

func mustWrite(t *testing.T, name, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, name string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(name, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func mustTimes(t *testing.T, name string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(name, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func payloadPath(base string, snap Snapshot, rel string) string {
	return filepath.Join(base, snap.ID, payloadDirName, filepath.FromSlash(rel))
}

func findEntry(t *testing.T, snap Snapshot, rel string) Entry {
	t.Helper()
	for _, e := range snap.Entries {
		if e.Path == rel {
			return e
		}
	}
	t.Fatalf("manifest has no entry %q", rel)
	return Entry{}
}

func assertEntryMetadata(t *testing.T, name string, entry Entry) {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&storableMode != os.FileMode(entry.Mode) {
		t.Fatalf("%s mode = %#o, want %#o", name, info.Mode()&storableMode, entry.Mode)
	}
	if !info.ModTime().Equal(entry.ModTime) {
		t.Fatalf("%s modtime = %v, want %v", name, info.ModTime(), entry.ModTime)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ai, bi)
}

func readManifest(t *testing.T, base string, snap Snapshot) manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(base, snap.ID, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeManifest(t *testing.T, base string, snap Snapshot, m manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, snap.ID, manifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertDoneProgress(t *testing.T, events []Progress, total int) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no progress events")
	}
	last := events[len(events)-1]
	if last.Phase != PhaseDone || last.Done != total || last.Total != total {
		t.Fatalf("final progress = %+v, want done with %d entries", last, total)
	}
	if last.BytesDone != last.BytesTotal {
		t.Fatalf("final byte progress = %d/%d", last.BytesDone, last.BytesTotal)
	}
	previous := 0
	for _, event := range events {
		if event.Done < previous {
			t.Fatalf("progress moved backwards: %+v", event)
		}
		previous = event.Done
	}
}

func TestCreatePreservesTreeAndWritesManifest(t *testing.T) {
	root, base := testTree(t)
	fixed := time.Unix(1_700_000_000, 123_456_789)
	mustMkdir(t, filepath.Join(root, "dir", "empty"), 0o750)
	mustWrite(t, filepath.Join(root, "dir", "file.txt"), "snapshot data", 0o640)
	mustSymlink(t, "dir/file.txt", filepath.Join(root, "file-link"))
	mustSymlink(t, "../missing", filepath.Join(root, "dangling-link"))
	mustTimes(t, filepath.Join(root, "dir", "file.txt"), fixed)
	mustTimes(t, filepath.Join(root, "dir", "empty"), fixed.Add(time.Second))
	mustTimes(t, filepath.Join(root, "dir"), fixed.Add(2*time.Second))
	mustTimes(t, root, fixed.Add(3*time.Second))

	var events []Progress
	snap, err := Create(root, base, func(p Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !validSnapshotID(snap.ID) || snap.CreatedAt.IsZero() || snap.Root == "" {
		t.Fatalf("invalid snapshot metadata: %+v", snap)
	}
	if snap.Files != 1 || snap.Directories != 3 || snap.Symlinks != 2 || snap.Bytes != int64(len("snapshot data")) {
		t.Fatalf("unexpected snapshot counts: %+v", snap)
	}
	if snap.Copied != 1 || snap.Reused != 0 {
		t.Fatalf("first snapshot copied/reused = %d/%d", snap.Copied, snap.Reused)
	}

	manifestPath := filepath.Join(base, snap.ID, manifestName)
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("manifest is not a regular file: %v", err)
	}
	m := readManifest(t, base, snap)
	if m.Version != manifestFormat || m.ID != snap.ID || len(m.Entries) != len(snap.Entries) {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	for _, rel := range []string{".", "dir", "dir/empty", "dir/file.txt"} {
		assertEntryMetadata(t, payloadPath(base, snap, rel), findEntry(t, snap, rel))
	}
	if got := mustRead(t, payloadPath(base, snap, "dir/file.txt")); got != "snapshot data" {
		t.Fatalf("payload data = %q", got)
	}
	for link, target := range map[string]string{"file-link": "dir/file.txt", "dangling-link": "../missing"} {
		got, err := os.Readlink(payloadPath(base, snap, link))
		if err != nil || got != target {
			t.Fatalf("payload symlink %s = %q, %v", link, got, err)
		}
	}
	if sameFile(t, filepath.Join(root, "dir", "file.txt"), payloadPath(base, snap, "dir/file.txt")) {
		t.Fatal("first snapshot must copy rather than hard-link the live file")
	}
	if events[0].Phase != PhaseScanning {
		t.Fatalf("first progress event = %+v", events[0])
	}
	assertDoneProgress(t, events, len(snap.Entries))

	// The initial copy isolates the snapshot from in-place live-tree writes.
	mustWrite(t, filepath.Join(root, "dir", "file.txt"), "live tree changed", 0o640)
	if got := mustRead(t, payloadPath(base, snap, "dir/file.txt")); got != "snapshot data" {
		t.Fatalf("live write modified snapshot: %q", got)
	}

	listed, err := List(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != snap.ID || !reflect.DeepEqual(listed[0].Entries, snap.Entries) {
		t.Fatalf("List = %+v, want snapshot %+v", listed, snap)
	}
}

func TestCreateReusesHardlinksAndCopiesMetadataChanges(t *testing.T) {
	root, base := testTree(t)
	fixed := time.Unix(1_700_100_000, 987_654_321)
	files := map[string]string{
		"stable.txt": "stable",
		"size.txt":   "aaaa",
		"time.txt":   "time",
		"mode.txt":   "mode",
	}
	for name, data := range files {
		mustWrite(t, filepath.Join(root, name), data, 0o644)
		mustTimes(t, filepath.Join(root, name), fixed)
	}

	first, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused != len(files) || second.Copied != 0 {
		t.Fatalf("unchanged snapshot copied/reused = %d/%d", second.Copied, second.Reused)
	}
	for name := range files {
		if !sameFile(t, payloadPath(base, first, name), payloadPath(base, second, name)) {
			t.Fatalf("unchanged %s was not hard-linked", name)
		}
	}

	// Change exactly one component of the size+modtime+mode reuse key per file.
	mustWrite(t, filepath.Join(root, "size.txt"), "longer", 0o644)
	mustTimes(t, filepath.Join(root, "size.txt"), fixed)
	mustTimes(t, filepath.Join(root, "time.txt"), fixed.Add(time.Second))
	if err := os.Chmod(filepath.Join(root, "mode.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.Reused != 1 || third.Copied != 3 {
		t.Fatalf("changed snapshot copied/reused = %d/%d", third.Copied, third.Reused)
	}
	if !sameFile(t, payloadPath(base, second, "stable.txt"), payloadPath(base, third, "stable.txt")) {
		t.Fatal("stable file was not hard-linked")
	}
	for _, name := range []string{"size.txt", "time.txt", "mode.txt"} {
		if sameFile(t, payloadPath(base, second, name), payloadPath(base, third, name)) {
			t.Fatalf("changed %s was incorrectly hard-linked", name)
		}
	}
	if got := mustRead(t, payloadPath(base, second, "size.txt")); got != "aaaa" {
		t.Fatalf("earlier snapshot was modified: %q", got)
	}
	if got := mustRead(t, payloadPath(base, third, "size.txt")); got != "longer" {
		t.Fatalf("changed payload = %q", got)
	}

	fourth, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.Reused != len(files) || fourth.Copied != 0 {
		t.Fatalf("post-change snapshot copied/reused = %d/%d", fourth.Copied, fourth.Reused)
	}
	for name := range files {
		if !sameFile(t, payloadPath(base, third, name), payloadPath(base, fourth, name)) {
			t.Fatalf("post-change %s was not hard-linked", name)
		}
	}

	listed, err := List(base)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{fourth.ID, third.ID, second.ID, first.ID}
	gotOrder := make([]string, len(listed))
	for i := range listed {
		gotOrder[i] = listed[i].ID
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("List order = %v, want %v", gotOrder, wantOrder)
	}

	err = filepath.WalkDir(payloadPath(base, third, "."), func(name string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(de.Name(), ".snapshot-") {
			t.Errorf("temporary file left in payload: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateHashesContentWhenSizeAndMetadataMatch(t *testing.T) {
	root, base := testTree(t)
	fixed := time.Unix(1_700_150_000, 123_456_789)
	name := filepath.Join(root, "same-size.txt")
	mustWrite(t, name, "before!", 0o640)
	mustTimes(t, name, fixed)
	first, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, name, "after!!", 0o640)
	mustTimes(t, name, fixed)
	second, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused != 0 || second.Copied != 1 {
		t.Fatalf("same-metadata content change copied/reused = %d/%d", second.Copied, second.Reused)
	}
	if got := mustRead(t, payloadPath(base, second, "same-size.txt")); got != "after!!" {
		t.Fatalf("new snapshot reused stale content %q", got)
	}
	if got := mustRead(t, payloadPath(base, first, "same-size.txt")); got != "before!" {
		t.Fatalf("earlier snapshot changed: %q", got)
	}
	if sameFile(t, payloadPath(base, first, "same-size.txt"), payloadPath(base, second, "same-size.txt")) {
		t.Fatal("changed content was hard-linked to the earlier snapshot")
	}

	third, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.Reused != 1 || third.Copied != 0 {
		t.Fatalf("unchanged content copied/reused = %d/%d", third.Copied, third.Reused)
	}
	if !sameFile(t, payloadPath(base, second, "same-size.txt"), payloadPath(base, third, "same-size.txt")) {
		t.Fatal("hash-verified unchanged content was not reused")
	}
}

func TestRestoreCopiesFilesAndPreservesDirectoriesAndSymlinks(t *testing.T) {
	root, base := testTree(t)
	fixed := time.Unix(1_700_200_000, 222_333_444)
	mustMkdir(t, filepath.Join(root, "dir", "empty"), 0o750)
	mustWrite(t, filepath.Join(root, "changed.txt"), "original", 0o640)
	mustWrite(t, filepath.Join(root, "deleted.txt"), "restore me", 0o644)
	mustWrite(t, filepath.Join(root, "dir", "nested.txt"), "nested original", 0o600)
	mustSymlink(t, "dir/nested.txt", filepath.Join(root, "link"))
	mustTimes(t, filepath.Join(root, "changed.txt"), fixed)
	mustTimes(t, filepath.Join(root, "deleted.txt"), fixed)
	mustTimes(t, filepath.Join(root, "dir", "nested.txt"), fixed)
	mustTimes(t, filepath.Join(root, "dir", "empty"), fixed.Add(time.Second))
	mustTimes(t, filepath.Join(root, "dir"), fixed.Add(2*time.Second))
	mustTimes(t, root, fixed.Add(3*time.Second))

	snap, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "changed.txt"), "modified", 0o640)
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "dir", "nested.txt"), "nested modified", 0o600)
	if err := os.Remove(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "changed.txt", filepath.Join(root, "link"))
	mustWrite(t, filepath.Join(root, "extra.txt"), "leave me", 0o644)

	var events []Progress
	if err := Restore(base, snap.ID, root, func(p Progress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}
	assertDoneProgress(t, events, len(snap.Entries))
	for name, want := range map[string]string{
		"changed.txt":    "original",
		"deleted.txt":    "restore me",
		"dir/nested.txt": "nested original",
		"extra.txt":      "leave me",
	} {
		if got := mustRead(t, filepath.Join(root, filepath.FromSlash(name))); got != want {
			t.Fatalf("restored %s = %q, want %q", name, got, want)
		}
	}
	if got, err := os.Readlink(filepath.Join(root, "link")); err != nil || got != "dir/nested.txt" {
		t.Fatalf("restored symlink = %q, %v", got, err)
	}
	for _, rel := range []string{".", "dir", "dir/empty", "changed.txt", "deleted.txt", "dir/nested.txt"} {
		assertEntryMetadata(t, filepath.Join(root, filepath.FromSlash(rel)), findEntry(t, snap, rel))
	}
	if sameFile(t, filepath.Join(root, "changed.txt"), payloadPath(base, snap, "changed.txt")) {
		t.Fatal("restore must copy the snapshot file, not hard-link it")
	}

	// A later live write must not alter the snapshot payload.
	mustWrite(t, filepath.Join(root, "changed.txt"), "later live write", 0o640)
	if got := mustRead(t, payloadPath(base, snap, "changed.txt")); got != "original" {
		t.Fatalf("restored live write modified snapshot: %q", got)
	}
	if err := Restore(base, snap.ID, root, nil); err != nil {
		t.Fatalf("second restore: %v", err)
	}

	newRoot := filepath.Join(t.TempDir(), "new-root")
	if err := Restore(base, snap.ID, newRoot, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(newRoot, "changed.txt")); got != "original" {
		t.Fatalf("new-root restore = %q", got)
	}
	if info, err := os.Stat(filepath.Join(newRoot, "dir", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory was not restored: %v", err)
	}
}

func TestRestoreReportsTypeConflictsWithoutFollowingSymlinks(t *testing.T) {
	source, base := testTree(t)
	mustWrite(t, filepath.Join(source, "file"), "saved", 0o644)
	mustWrite(t, filepath.Join(source, "safe"), "saved safe", 0o644)
	mustMkdir(t, filepath.Join(source, "parent"), 0o755)
	mustWrite(t, filepath.Join(source, "parent", "child"), "child", 0o644)
	mustSymlink(t, "file", filepath.Join(source, "link"))
	snap, err := Create(source, base, nil)
	if err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "sentinel"), "outside", 0o644)
	mustSymlink(t, filepath.Join(outside, "sentinel"), filepath.Join(destination, "file"))
	mustSymlink(t, outside, filepath.Join(destination, "parent"))
	mustWrite(t, filepath.Join(destination, "link"), "not a symlink", 0o644)
	mustWrite(t, filepath.Join(destination, "safe"), "destination safe", 0o644)

	var events []Progress
	err = Restore(base, snap.ID, destination, func(p Progress) { events = append(events, p) })
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("Restore error = %v, want ConflictError", err)
	}
	if len(conflictErr.Conflicts) != 3 {
		t.Fatalf("conflicts = %+v", conflictErr.Conflicts)
	}
	paths := make(map[string]bool)
	for _, c := range conflictErr.Conflicts {
		paths[c.Path] = true
	}
	for _, want := range []string{"file", "parent", "link"} {
		if !paths[want] {
			t.Fatalf("missing conflict for %q: %+v", want, conflictErr.Conflicts)
		}
	}
	conflictEvents := 0
	for _, event := range events {
		if event.Phase == PhaseConflict {
			conflictEvents++
		}
	}
	if conflictEvents != 3 {
		t.Fatalf("conflict progress events = %d", conflictEvents)
	}
	if got := mustRead(t, filepath.Join(outside, "sentinel")); got != "outside" {
		t.Fatalf("outside file changed: %q", got)
	}
	if got, err := os.Readlink(filepath.Join(destination, "file")); err != nil || got != filepath.Join(outside, "sentinel") {
		t.Fatalf("conflicting symlink was replaced: %q, %v", got, err)
	}
	if got, err := os.Readlink(filepath.Join(destination, "parent")); err != nil || got != outside {
		t.Fatalf("conflicting parent symlink was replaced: %q, %v", got, err)
	}
	if got := mustRead(t, filepath.Join(destination, "link")); got != "not a symlink" {
		t.Fatalf("conflicting regular file changed: %q", got)
	}
	if got := mustRead(t, filepath.Join(destination, "safe")); got != "destination safe" {
		t.Fatalf("nonconflicting file changed before conflict rejection: %q", got)
	}
}

func TestRestoreRejectsSymlinkRoot(t *testing.T) {
	source, base := testTree(t)
	mustWrite(t, filepath.Join(source, "file"), "saved", 0o644)
	snap, err := Create(source, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "file"), "outside", 0o644)
	rootLink := filepath.Join(t.TempDir(), "root-link")
	mustSymlink(t, outside, rootLink)
	if err := Restore(base, snap.ID, rootLink, nil); err == nil {
		t.Fatal("Restore accepted a symlink root")
	}
	if got := mustRead(t, filepath.Join(outside, "file")); got != "outside" {
		t.Fatalf("symlink root target changed: %q", got)
	}
}

func TestRestoreRootRejectsReplacementBeforeOpen(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	moved := filepath.Join(parent, "moved")
	outside := filepath.Join(parent, "outside")
	mustMkdir(t, root, 0o755)
	mustMkdir(t, outside, 0o755)
	resolved, expected, err := prepareRootDir(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(resolved, moved); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, outside, resolved)
	if opened, err := openDirectoryRootWithInfo(resolved, expected); err == nil {
		opened.Close()
		t.Fatal("restore root replacement was accepted after identity validation")
	}
}

func TestRestorePinsOpenedRootAcrossPathReplacement(t *testing.T) {
	source, base := testTree(t)
	mustWrite(t, filepath.Join(source, "file"), "saved", 0o644)
	snap, err := Create(source, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	moved := filepath.Join(parent, "moved")
	outside := filepath.Join(parent, "outside")
	mustWrite(t, filepath.Join(destination, "file"), "changed", 0o644)
	mustWrite(t, filepath.Join(outside, "file"), "outside", 0o644)
	replaced := false
	var replaceErr error
	err = Restore(base, snap.ID, destination, func(p Progress) {
		if p.Phase == PhaseRestoring && !replaced {
			replaced = true
			if err := os.Rename(destination, moved); err != nil {
				replaceErr = err
				return
			}
			replaceErr = os.Symlink(outside, destination)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaceErr != nil {
		t.Fatal(replaceErr)
	}
	if got := mustRead(t, filepath.Join(moved, "file")); got != "saved" {
		t.Fatalf("pinned restore root contains %q, want saved", got)
	}
	if got := mustRead(t, filepath.Join(outside, "file")); got != "outside" {
		t.Fatalf("replacement symlink target changed: %q", got)
	}
}

func TestCreateDoesNotFollowSymlinksAndExcludesSystemAndBaseDirectories(t *testing.T) {
	root, _ := testTree(t)
	base := filepath.Join(root, ".snapshots")
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret"), "outside", 0o644)
	mustSymlink(t, outside, filepath.Join(root, "outside-link"))
	for _, name := range []string{"proc", "sys", "dev", "run", "tmp"} {
		mustWrite(t, filepath.Join(root, name, "ignored"), name, 0o644)
	}
	mustWrite(t, filepath.Join(root, "keep", "tmp", "included"), "included", 0o644)

	first, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"proc", "sys", "dev", "run", "tmp", ".snapshots", "outside-link/secret"} {
		for _, entry := range first.Entries {
			if entry.Path == rel || strings.HasPrefix(entry.Path, rel+"/") {
				t.Fatalf("excluded path %q appears as %q", rel, entry.Path)
			}
		}
	}
	if entry := findEntry(t, first, "outside-link"); entry.Type != EntrySymlink || entry.Target != outside {
		t.Fatalf("outside-link entry = %+v", entry)
	}
	if entry := findEntry(t, first, "keep/tmp/included"); entry.Type != EntryFile {
		t.Fatalf("nested tmp entry = %+v", entry)
	}
	second, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Files != 1 || second.Reused != 1 {
		t.Fatalf("snapshot recursively included its base or changed metadata: %+v", second)
	}
	mustWrite(t, filepath.Join(root, "keep", "tmp", "included"), "changed", 0o644)
	if err := Restore(base, first.ID, root, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(root, "keep", "tmp", "included")); got != "included" {
		t.Fatalf("restored included file = %q", got)
	}
	for _, name := range []string{"proc", "sys", "dev", "run", "tmp"} {
		if got := mustRead(t, filepath.Join(root, name, "ignored")); got != name {
			t.Fatalf("excluded %s file changed: %q", name, got)
		}
	}
	if got := mustRead(t, filepath.Join(outside, "secret")); got != "outside" {
		t.Fatalf("symlink target changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, first.ID, manifestName)); err != nil {
		t.Fatalf("restore removed snapshot base: %v", err)
	}
	if _, err := Create(root, root, nil); err == nil {
		t.Fatal("Create accepted the live root as its snapshot base")
	}
}

func TestListHandlesMissingIncompleteAndMalformedSnapshots(t *testing.T) {
	root, base := testTree(t)
	listed, err := List(base)
	if err != nil || len(listed) != 0 {
		t.Fatalf("List(missing) = %+v, %v", listed, err)
	}
	mustWrite(t, filepath.Join(root, "file"), "data", 0o644)
	first, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(base, "unrelated"), "junk", 0o644)
	mustMkdir(t, filepath.Join(base, "incomplete"), 0o755)
	mustMkdir(t, filepath.Join(base, ".tmp-staging"), 0o755)
	mustWrite(t, filepath.Join(base, ".tmp-staging", manifestName), "not json", 0o600)
	mustSymlink(t, first.ID, filepath.Join(base, "snapshot-link"))

	listed, err = List(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID {
		t.Fatalf("List = %+v", listed)
	}
	mustWrite(t, filepath.Join(base, second.ID, manifestName), "{", 0o600)
	if _, err := List(base); err == nil {
		t.Fatal("List accepted a malformed manifest")
	}
	if err := Restore(base, second.ID, t.TempDir(), nil); err == nil {
		t.Fatal("Restore accepted a malformed manifest")
	}
	if err := Restore(base, "../first", t.TempDir(), nil); err == nil {
		t.Fatal("Restore accepted a path-traversal snapshot ID")
	}
}

func TestRestoreRejectsUnsafeManifestPaths(t *testing.T) {
	tests := map[string]func(*manifest){
		"parent traversal": func(m *manifest) {
			for i := range m.Entries {
				if m.Entries[i].Type == EntryFile {
					m.Entries[i].Path = "../escaped"
					return
				}
			}
		},
		"absolute path": func(m *manifest) {
			for i := range m.Entries {
				if m.Entries[i].Type == EntryFile {
					m.Entries[i].Path = "/tmp/escaped"
					return
				}
			}
		},
		"duplicate path": func(m *manifest) {
			for i := range m.Entries {
				if m.Entries[i].Type == EntryFile {
					m.Entries = append(m.Entries, m.Entries[i])
					return
				}
			}
		},
		"missing parent": func(m *manifest) {
			for i := range m.Entries {
				if m.Entries[i].Type == EntryFile {
					m.Entries[i].Path = "ghost/child"
					return
				}
			}
		},
		"missing root": func(m *manifest) {
			for i := range m.Entries {
				if m.Entries[i].Path == "." {
					m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
					return
				}
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			source, base := testTree(t)
			mustWrite(t, filepath.Join(source, "safe"), "saved", 0o644)
			snap, err := Create(source, base, nil)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, base, snap)
			mutate(&m)
			writeManifest(t, base, snap, m)
			destination := t.TempDir()
			mustWrite(t, filepath.Join(destination, "sentinel"), "unchanged", 0o644)
			if err := Restore(base, snap.ID, destination, nil); err == nil {
				t.Fatal("Restore accepted an unsafe manifest")
			}
			if got := mustRead(t, filepath.Join(destination, "sentinel")); got != "unchanged" {
				t.Fatalf("destination changed after rejected manifest: %q", got)
			}
		})
	}
}

func TestRestoreRejectsSymlinkPayloadAndManifest(t *testing.T) {
	t.Run("payload file", func(t *testing.T) {
		source, base := testTree(t)
		mustWrite(t, filepath.Join(source, "victim"), "saved", 0o644)
		snap, err := Create(source, base, nil)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "secret")
		mustWrite(t, outside, "outside", 0o644)
		payloadFile := payloadPath(base, snap, "victim")
		if err := os.Remove(payloadFile); err != nil {
			t.Fatal(err)
		}
		mustSymlink(t, outside, payloadFile)
		destination := t.TempDir()
		mustWrite(t, filepath.Join(destination, "victim"), "destination", 0o644)
		if err := Restore(base, snap.ID, destination, nil); err == nil {
			t.Fatal("Restore followed a symlink in the snapshot payload")
		}
		if got := mustRead(t, filepath.Join(destination, "victim")); got != "destination" {
			t.Fatalf("destination changed: %q", got)
		}
		if got := mustRead(t, outside); got != "outside" {
			t.Fatalf("outside file changed: %q", got)
		}
	})

	t.Run("manifest", func(t *testing.T) {
		source, base := testTree(t)
		mustWrite(t, filepath.Join(source, "file"), "saved", 0o644)
		snap, err := Create(source, base, nil)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "manifest")
		mustWrite(t, outside, "outside", 0o600)
		manifestPath := filepath.Join(base, snap.ID, manifestName)
		if err := os.Remove(manifestPath); err != nil {
			t.Fatal(err)
		}
		mustSymlink(t, outside, manifestPath)
		if _, err := List(base); err == nil {
			t.Fatal("List followed a symlink manifest")
		}
		if err := Restore(base, snap.ID, t.TempDir(), nil); err == nil {
			t.Fatal("Restore followed a symlink manifest")
		}
		if got := mustRead(t, outside); got != "outside" {
			t.Fatalf("outside manifest changed: %q", got)
		}
	})
}

func TestCreateDoesNotReuseAcrossDifferentRoots(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "snapshots")
	rootA := filepath.Join(parent, "a")
	rootB := filepath.Join(parent, "b")
	fixed := time.Unix(1_700_300_000, 0)
	mustWrite(t, filepath.Join(rootA, "file"), "alpha", 0o644)
	mustWrite(t, filepath.Join(rootB, "file"), "bravo", 0o644)
	mustTimes(t, filepath.Join(rootA, "file"), fixed)
	mustTimes(t, filepath.Join(rootB, "file"), fixed)
	first, err := Create(rootA, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(rootB, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused != 0 || second.Copied != 1 {
		t.Fatalf("different roots copied/reused = %d/%d", second.Copied, second.Reused)
	}
	if sameFile(t, payloadPath(base, first, "file"), payloadPath(base, second, "file")) {
		t.Fatal("snapshots from different roots were hard-linked")
	}
	if got := mustRead(t, payloadPath(base, second, "file")); got != "bravo" {
		t.Fatalf("second root payload = %q", got)
	}
}

func TestEmptyRootSnapshotCanBeListedAndRestored(t *testing.T) {
	root, base := testTree(t)
	snap, err := Create(root, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 1 || snap.Entries[0].Path != "." || snap.Entries[0].Type != EntryDirectory {
		t.Fatalf("empty snapshot entries = %+v", snap.Entries)
	}
	listed, err := List(base)
	if err != nil || len(listed) != 1 {
		t.Fatalf("List = %+v, %v", listed, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if err := Restore(base, snap.ID, destination, nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		t.Fatalf("restored empty root is not a directory: %v", err)
	}
}

func TestConflictErrorMessageContainsPath(t *testing.T) {
	err := &ConflictError{Conflicts: []Conflict{{Path: "dir", Existing: "symlink", Required: "directory"}}}
	if !strings.Contains(err.Error(), "dir") || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("unexpected conflict error: %v", err)
	}
}

func ExampleCreate() {
	root, err := os.MkdirTemp("", "snapshot-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	base, err := os.MkdirTemp("", "snapshot-base-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(base)
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("data"), 0o644); err != nil {
		panic(err)
	}
	snap, err := Create(root, base, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(snap.Files)
	// Output: 1
}
