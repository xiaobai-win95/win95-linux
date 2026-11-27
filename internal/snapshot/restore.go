package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Restore copies a snapshot into root. Existing regular files are atomically
// replaced, symbolic links are recreated without following them, and directory
// modes and modification times are restored after their children. Snapshot
// files are copied, not hard-linked, so subsequent live-tree edits cannot
// damage the snapshot.
//
// Restore overlays the snapshot: unrelated destination files are left alone.
// Before changing the destination, Restore checks every snapshot path. An
// existing object of an incompatible type (including a symlink where a real
// directory is required) is returned as a ConflictError and no files are
// restored. Changed regular files and changed symlink targets are not
// conflicts; both are expected during a rollback.
func Restore(baseDir, id, root string, progress func(Progress)) error {
	if !validSnapshotID(id) {
		return fmt.Errorf("invalid snapshot ID %q", id)
	}
	base, err := prepareBaseDir(baseDir, false)
	if err != nil {
		return err
	}
	snap, err := readSnapshot(base, id)
	if err != nil {
		return err
	}
	snapshotDir := filepath.Join(base, id)
	payloadDir := filepath.Join(snapshotDir, payloadDirName)
	payload, err := openDirectoryRoot(payloadDir)
	if err != nil {
		return fmt.Errorf("open snapshot payload: %w", err)
	}
	defer payload.Close()
	if err := verifyPayload(payload, snap.Entries); err != nil {
		return fmt.Errorf("verify snapshot payload: %w", err)
	}

	resolvedRoot, rootInfo, err := prepareRootDir(root, true)
	if err != nil {
		return err
	}
	if excludedSystemPath(resolvedRoot) {
		return fmt.Errorf("restore root %q is excluded", resolvedRoot)
	}
	if resolvedRoot == base || pathWithin(snapshotDir, resolvedRoot) || pathWithin(payloadDir, resolvedRoot) {
		return fmt.Errorf("restore root must not be a snapshot directory")
	}
	for _, e := range snap.Entries {
		dest := filepath.Join(resolvedRoot, filepath.FromSlash(e.Path))
		if excludedRootPath(e.Path) || (excludedSystemPath(dest) && pathWithin(resolvedRoot, dest)) {
			return fmt.Errorf("snapshot entry %q targets an excluded path", e.Path)
		}
		if pathWithin(base, dest) {
			return fmt.Errorf("snapshot entry %q would overwrite the snapshot base", e.Path)
		}
	}

	destination, err := openDirectoryRootWithInfo(resolvedRoot, rootInfo)
	if err != nil {
		return fmt.Errorf("open restore root: %w", err)
	}
	defer destination.Close()

	conflicts := findConflicts(destination, sortedEntries(snap.Entries, false))
	for _, c := range conflicts {
		report(progress, Progress{Phase: PhaseConflict, Path: c.Path})
	}
	if len(conflicts) != 0 {
		return &ConflictError{Conflicts: conflicts}
	}

	var bytesTotal int64
	for _, e := range snap.Entries {
		if e.Type == EntryFile {
			bytesTotal += e.Size
		}
	}
	var bytesDone int64
	copied := 0
	done := 0

	// Make directories writable while restoring their children. Final modes are
	// applied deepest-first after all files and links are in place.
	for _, e := range sortedEntries(snap.Entries, false) {
		if e.Type != EntryDirectory {
			continue
		}
		if err := makeDirectoryWritable(destination, e.Path); err != nil {
			return fmt.Errorf("prepare directory %q: %w", e.Path, err)
		}
		done++
		report(progress, Progress{
			Phase:      PhaseRestoring,
			Path:       e.Path,
			Done:       done,
			Total:      len(snap.Entries),
			BytesDone:  bytesDone,
			BytesTotal: bytesTotal,
			Copied:     copied,
		})
	}

	for _, e := range sortedEntries(snap.Entries, false) {
		switch e.Type {
		case EntryDirectory:
			continue
		case EntryFile:
			if err := restoreFile(payload, destination, e); err != nil {
				return fmt.Errorf("restore file %q: %w", e.Path, err)
			}
			bytesDone += e.Size
			copied++
		case EntrySymlink:
			if err := restoreSymlink(destination, e); err != nil {
				return fmt.Errorf("restore symlink %q: %w", e.Path, err)
			}
		}
		done++
		report(progress, Progress{
			Phase:      PhaseRestoring,
			Path:       e.Path,
			Done:       done,
			Total:      len(snap.Entries),
			BytesDone:  bytesDone,
			BytesTotal: bytesTotal,
			Copied:     copied,
		})
	}

	for _, e := range sortedEntries(snap.Entries, true) {
		if e.Type != EntryDirectory {
			continue
		}
		info, err := lstatNoSymlinkParents(destination, e.Path)
		if err != nil {
			return fmt.Errorf("inspect restored directory %q: %w", e.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("restored directory %q has an incompatible type", e.Path)
		}
		if err := destination.Chmod(filepath.FromSlash(e.Path), os.FileMode(e.Mode)); err != nil {
			return fmt.Errorf("restore directory mode %q: %w", e.Path, err)
		}
		if err := destination.Chtimes(filepath.FromSlash(e.Path), e.ModTime, e.ModTime); err != nil {
			return fmt.Errorf("restore directory time %q: %w", e.Path, err)
		}
	}
	report(progress, Progress{
		Phase:      PhaseDone,
		Path:       id,
		Done:       done,
		Total:      len(snap.Entries),
		BytesDone:  bytesDone,
		BytesTotal: bytesTotal,
		Copied:     copied,
	})
	return nil
}

func verifyPayload(root *os.Root, entries []Entry) error {
	for _, e := range entries {
		info, err := lstatNoSymlinkParents(root, e.Path)
		if err != nil {
			return fmt.Errorf("%q: %w", e.Path, err)
		}
		switch e.Type {
		case EntryDirectory:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !metadataMatches(info, e) {
				return fmt.Errorf("%q does not match the manifest directory", e.Path)
			}
		case EntryFile:
			if !fileMetadataMatches(info, e) {
				return fmt.Errorf("%q does not match the manifest file", e.Path)
			}
		case EntrySymlink:
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("%q is not a symbolic link", e.Path)
			}
			target, err := root.Readlink(filepath.FromSlash(e.Path))
			if err != nil {
				return fmt.Errorf("%q: %w", e.Path, err)
			}
			if target != e.Target {
				return fmt.Errorf("%q does not match the manifest symlink target", e.Path)
			}
		}
	}
	return nil
}

// inspectDestination returns missing=true when rel or one of its parents does
// not exist. conflictAt is set when an existing parent is not a real directory.
func inspectDestination(root *os.Root, rel string) (info os.FileInfo, missing bool, conflictAt string) {
	if rel == "." {
		info, err := root.Lstat(".")
		if err != nil {
			return nil, false, "."
		}
		return info, false, ""
	}
	parts := strings.Split(rel, "/")
	current := ""
	var err error
	for i, part := range parts {
		current = path.Join(current, part)
		info, err = root.Lstat(filepath.FromSlash(current))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, true, ""
		}
		if err != nil {
			return nil, false, current
		}
		if i < len(parts)-1 && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return nil, false, current
		}
	}
	return info, false, ""
}

func findConflicts(root *os.Root, entries []Entry) []Conflict {
	seen := make(map[string]bool)
	conflicts := make([]Conflict, 0)
	for _, e := range entries {
		info, missing, conflictAt := inspectDestination(root, e.Path)
		if conflictAt != "" {
			if !seen[conflictAt] {
				conflicts = append(conflicts, Conflict{
					Path:     conflictAt,
					Existing: "unreadable or non-directory entry",
					Required: string(EntryDirectory),
				})
				seen[conflictAt] = true
			}
			continue
		}
		if missing {
			continue
		}
		compatible := false
		switch e.Type {
		case EntryDirectory:
			compatible = info.IsDir() && info.Mode()&os.ModeSymlink == 0
		case EntryFile:
			compatible = info.Mode().IsRegular()
		case EntrySymlink:
			compatible = info.Mode()&os.ModeSymlink != 0
		}
		if !compatible && !seen[e.Path] {
			conflicts = append(conflicts, Conflict{
				Path:     e.Path,
				Existing: objectType(info),
				Required: string(e.Type),
			})
			seen[e.Path] = true
		}
	}
	return conflicts
}

func makeDirectoryWritable(root *os.Root, rel string) error {
	name := filepath.FromSlash(rel)
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("not a real directory")
	}
	return root.Chmod(name, (info.Mode()&storableMode)|0o700)
}

func restoreFile(source, destination *os.Root, entry Entry) error {
	sourceInfo, err := lstatNoSymlinkParents(source, entry.Path)
	if err != nil {
		return err
	}
	if !fileMetadataMatches(sourceInfo, entry) {
		return fmt.Errorf("snapshot source changed")
	}
	src, err := source.Open(filepath.FromSlash(entry.Path))
	if err != nil {
		return err
	}
	defer src.Close()
	opened, err := src.Stat()
	if err != nil {
		return err
	}
	if !fileMetadataMatches(opened, entry) || !os.SameFile(sourceInfo, opened) {
		return fmt.Errorf("snapshot source changed while opening")
	}

	destName := filepath.FromSlash(entry.Path)
	parent := path.Dir(entry.Path)
	if parent == "." {
		parent = ""
	}
	tmpName, dst, err := createTempInRoot(destination, parent)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = dst.Close()
			_ = destination.Remove(filepath.FromSlash(tmpName))
		}
	}()
	if err := copyAndSetMetadata(src, dst, opened, entry); err != nil {
		return err
	}
	after, err := src.Stat()
	if err != nil {
		return err
	}
	if !fileMetadataMatches(after, entry) || !os.SameFile(opened, after) {
		return fmt.Errorf("snapshot source changed while copying")
	}
	if err := destination.Chtimes(filepath.FromSlash(tmpName), entry.ModTime, entry.ModTime); err != nil {
		return err
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := destination.Rename(filepath.FromSlash(tmpName), destName); err != nil {
		return err
	}
	keep = true
	return nil
}

func restoreSymlink(destination *os.Root, entry Entry) error {
	parent := path.Dir(entry.Path)
	if parent == "." {
		parent = ""
	}
	for attempts := 0; attempts < 16; attempts++ {
		suffix, err := randomSuffix(8)
		if err != nil {
			return err
		}
		tmpName := path.Join(parent, ".snapshot-link-"+suffix)
		if err := destination.Symlink(entry.Target, filepath.FromSlash(tmpName)); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return err
		}
		if err := destination.Rename(filepath.FromSlash(tmpName), filepath.FromSlash(entry.Path)); err != nil {
			_ = destination.Remove(filepath.FromSlash(tmpName))
			return err
		}
		return nil
	}
	return fmt.Errorf("could not allocate a temporary symlink name")
}

func createTempInRoot(root *os.Root, parent string) (string, *os.File, error) {
	for attempts := 0; attempts < 16; attempts++ {
		suffix, err := randomSuffix(8)
		if err != nil {
			return "", nil, err
		}
		name := path.Join(parent, ".snapshot-restore-"+suffix)
		f, err := root.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return name, f, err
	}
	return "", nil, fmt.Errorf("could not allocate a temporary file name")
}
