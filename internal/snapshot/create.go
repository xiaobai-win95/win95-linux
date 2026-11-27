package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Create records root in baseDir. The first snapshot copies regular files.
// Later snapshots of the same root hard-link files only when size, modification
// time, permission mode, and SHA-256 content match the immediately preceding
// snapshot. Changed files are copied through a temporary file and atomically
// renamed into place.
// Symbolic links are recorded as links and are never traversed. The top-level
// proc, sys, dev, run, and tmp directories, and baseDir itself when it is below
// root, are excluded. Special files (devices, sockets, and FIFOs) are skipped.
func Create(root, baseDir string, progress func(Progress)) (Snapshot, error) {
	resolvedRoot, rootInfo, err := prepareRootDir(root, false)
	if err != nil {
		return Snapshot{}, err
	}
	if excludedSystemPath(resolvedRoot) {
		return Snapshot{}, fmt.Errorf("root %q is excluded from snapshots", resolvedRoot)
	}
	base, err := prepareBaseDir(baseDir, true)
	if err != nil {
		return Snapshot{}, err
	}
	if resolvedRoot == base {
		return Snapshot{}, fmt.Errorf("snapshot base must differ from the snapshot root")
	}

	snapshots, err := List(base)
	if err != nil {
		return Snapshot{}, err
	}
	var previous *Snapshot
	for i := range snapshots {
		if snapshots[i].Root == resolvedRoot {
			previous = &snapshots[i]
			break
		}
	}
	previousEntries := make(map[string]Entry)
	if previous != nil {
		for _, e := range previous.Entries {
			previousEntries[e.Path] = e
		}
	}

	createdAt := time.Now().UTC()
	id, err := newSnapshotID(base, createdAt)
	if err != nil {
		return Snapshot{}, err
	}
	staging, err := os.MkdirTemp(base, ".tmp-")
	if err != nil {
		return Snapshot{}, fmt.Errorf("create snapshot staging directory: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	payload := filepath.Join(staging, payloadDirName)
	if err := os.Mkdir(payload, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("create snapshot payload: %w", err)
	}

	rootHandle, err := openDirectoryRootWithInfo(resolvedRoot, rootInfo)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open snapshot root: %w", err)
	}
	defer rootHandle.Close()

	var previousRoot *os.Root
	var previousPayload string
	if previous != nil {
		previousPayload = filepath.Join(base, previous.ID, payloadDirName)
		previousRoot, err = openDirectoryRoot(previousPayload)
		if err != nil {
			return Snapshot{}, fmt.Errorf("open previous snapshot payload: %w", err)
		}
		defer previousRoot.Close()
	}

	snap := Snapshot{
		ID:        id,
		CreatedAt: createdAt,
		Root:      resolvedRoot,
		Entries:   make([]Entry, 0),
	}
	report(progress, Progress{Phase: PhaseScanning, Path: "."})
	var bytesDone int64
	err = filepath.WalkDir(resolvedRoot, func(full string, de fs.DirEntry, walkErr error) error {
		relOS, relErr := filepath.Rel(resolvedRoot, full)
		if relErr != nil {
			return relErr
		}
		rel := filepath.ToSlash(relOS)
		if shouldExclude(resolvedRoot, base, full) {
			report(progress, Progress{Phase: PhaseSkipped, Path: rel})
			if walkErr == nil && de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if walkErr != nil {
			return fmt.Errorf("walk %q: %w", rel, walkErr)
		}
		info, err := de.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", rel, err)
		}

		entry := Entry{
			Path:    rel,
			Type:    EntryDirectory,
			Mode:    uint32(info.Mode() & storableMode),
			ModTime: info.ModTime(),
		}
		dest := filepath.Join(payload, filepath.FromSlash(rel))
		phase := PhaseCopying
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return fmt.Errorf("read symlink %q: %w", rel, err)
			}
			entry.Type = EntrySymlink
			entry.Target = target
			if err := os.Symlink(target, dest); err != nil {
				return fmt.Errorf("store symlink %q: %w", rel, err)
			}
			snap.Symlinks++
		case info.IsDir():
			phase = PhaseCopying
			if rel != "." {
				if err := os.Mkdir(dest, 0o700); err != nil {
					return fmt.Errorf("store directory %q: %w", rel, err)
				}
			}
			snap.Directories++
		case info.Mode().IsRegular():
			entry.Type = EntryFile
			entry.Size = info.Size()
			old, hasOld := previousEntries[rel]
			reused := false
			if previousRoot != nil && hasOld && old.Type == EntryFile && fileMetadataMatches(info, old) {
				currentInfo, err := lstatNoSymlinkParents(rootHandle, rel)
				if err != nil {
					return fmt.Errorf("recheck %q: %w", rel, err)
				}
				if !fileMetadataMatches(currentInfo, entry) {
					return fmt.Errorf("file %q changed while walking", rel)
				}
				reused, err = reuseFile(rootHandle, previousRoot, previousPayload, rel, dest, entry)
				if err != nil {
					return fmt.Errorf("reuse %q: %w", rel, err)
				}
			}
			if !reused {
				if err := copyLiveFile(rootHandle, rel, dest, entry); err != nil {
					return fmt.Errorf("copy %q: %w", rel, err)
				}
				snap.Copied++
			} else {
				snap.Reused++
			}
			if reused {
				phase = PhaseLinking
			} else {
				phase = PhaseCopying
			}
			snap.Files++
			snap.Bytes += entry.Size
			bytesDone += entry.Size
		default:
			report(progress, Progress{Phase: PhaseSkipped, Path: rel})
			return nil
		}

		snap.Entries = append(snap.Entries, entry)
		report(progress, Progress{
			Phase:      phase,
			Path:       rel,
			Done:       len(snap.Entries),
			BytesDone:  bytesDone,
			BytesTotal: snap.Bytes,
			Reused:     snap.Reused,
			Copied:     snap.Copied,
		})
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	if len(snap.Entries) == 0 {
		return Snapshot{}, fmt.Errorf("snapshot root contains no entries")
	}

	// Populate directories with temporary owner permissions, then restore their
	// metadata after all children have been created.
	dirs := make([]Entry, 0, snap.Directories)
	for _, e := range snap.Entries {
		if e.Type == EntryDirectory {
			dirs = append(dirs, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := entryDepth(dirs[i]), entryDepth(dirs[j])
		if di != dj {
			return di > dj
		}
		return dirs[i].Path > dirs[j].Path
	})
	for _, e := range dirs {
		p := filepath.Join(payload, filepath.FromSlash(e.Path))
		info, err := os.Lstat(p)
		if err != nil {
			return Snapshot{}, fmt.Errorf("inspect stored directory %q: %w", e.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Snapshot{}, fmt.Errorf("stored directory %q changed during snapshot", e.Path)
		}
		if err := os.Chmod(p, os.FileMode(e.Mode)); err != nil {
			return Snapshot{}, fmt.Errorf("restore directory mode %q: %w", e.Path, err)
		}
		if err := os.Chtimes(p, e.ModTime, e.ModTime); err != nil {
			return Snapshot{}, fmt.Errorf("restore directory time %q: %w", e.Path, err)
		}
	}

	m := manifest{Version: manifestFormat, Snapshot: snap}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(staging, manifestName), data, 0o600); err != nil {
		return Snapshot{}, fmt.Errorf("write manifest: %w", err)
	}
	final := filepath.Join(base, id)
	if err := os.Rename(staging, final); err != nil {
		return Snapshot{}, fmt.Errorf("publish snapshot: %w", err)
	}
	committed = true
	report(progress, Progress{
		Phase:      PhaseDone,
		Path:       id,
		Done:       len(snap.Entries),
		Total:      len(snap.Entries),
		BytesDone:  bytesDone,
		BytesTotal: snap.Bytes,
		Reused:     snap.Reused,
		Copied:     snap.Copied,
	})
	return snap, nil
}

func newSnapshotID(base string, now time.Time) (string, error) {
	for attempts := 0; attempts < 8; attempts++ {
		suffix, err := randomSuffix(6)
		if err != nil {
			return "", fmt.Errorf("generate snapshot ID: %w", err)
		}
		id := now.Format("20060102T150405.000000000Z") + "-" + suffix
		if !validSnapshotID(id) {
			continue
		}
		_, err = os.Lstat(filepath.Join(base, id))
		if errors.Is(err, fs.ErrNotExist) {
			return id, nil
		}
		if err != nil {
			return "", fmt.Errorf("check snapshot ID: %w", err)
		}
	}
	return "", errors.New("could not generate a unique snapshot ID")
}

func shouldExclude(root, base, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err == nil && excludedRootPath(filepath.ToSlash(rel)) {
		return true
	}
	if excludedSystemPath(full) && pathWithin(root, filepath.Clean(full)) {
		return true
	}
	return base != root && pathWithin(root, base) && pathWithin(base, full)
}

func excludedSystemPath(full string) bool {
	clean := filepath.Clean(full)
	switch clean {
	case "/proc", "/sys", "/dev", "/run", "/tmp":
		return true
	default:
		return false
	}
}

func reuseFile(currentRoot, previousRoot *os.Root, previousPayload, rel, dest string, entry Entry) (bool, error) {
	currentInfo, err := lstatNoSymlinkParents(currentRoot, rel)
	if err != nil {
		return false, err
	}
	if !fileMetadataMatches(currentInfo, entry) {
		return false, fmt.Errorf("file changed while walking")
	}
	previousInfo, err := lstatNoSymlinkParents(previousRoot, rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !fileMetadataMatches(previousInfo, entry) {
		return false, nil
	}
	currentHash, err := fileContentHash(currentRoot, rel, entry)
	if err != nil {
		return false, err
	}
	previousHash, err := fileContentHash(previousRoot, rel, entry)
	if err != nil {
		return false, err
	}
	if currentHash != previousHash {
		return false, nil
	}
	source := filepath.Join(previousPayload, filepath.FromSlash(rel))
	if err := os.Link(source, dest); err != nil {
		return false, err
	}
	stored, err := os.Lstat(dest)
	if err != nil {
		_ = os.Remove(dest)
		return false, err
	}
	if !fileMetadataMatches(stored, entry) || !os.SameFile(stored, previousInfo) {
		_ = os.Remove(dest)
		return false, fmt.Errorf("hard-linked payload does not match the source file")
	}
	return true, nil
}

func fileContentHash(root *os.Root, rel string, entry Entry) (string, error) {
	expected, err := lstatNoSymlinkParents(root, rel)
	if err != nil {
		return "", err
	}
	if !fileMetadataMatches(expected, entry) {
		return "", fmt.Errorf("file changed before hashing")
	}
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fileMetadataMatches(opened, entry) || !os.SameFile(expected, opened) {
		return "", fmt.Errorf("file changed while opening for hashing")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, f)
	if err != nil {
		return "", err
	}
	if written != entry.Size {
		return "", fmt.Errorf("hashed %d bytes, expected %d", written, entry.Size)
	}
	after, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fileMetadataMatches(after, entry) || !os.SameFile(opened, after) {
		return "", fmt.Errorf("file changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyLiveFile(root *os.Root, rel, dest string, entry Entry) error {
	info, err := lstatNoSymlinkParents(root, rel)
	if err != nil {
		return err
	}
	if !fileMetadataMatches(info, entry) {
		return fmt.Errorf("file changed while walking")
	}
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !fileMetadataMatches(opened, entry) || !os.SameFile(info, opened) {
		return fmt.Errorf("file changed while opening")
	}
	if err := copyToPath(f, opened, dest, entry); err != nil {
		return err
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if !fileMetadataMatches(after, entry) || !os.SameFile(opened, after) {
		return fmt.Errorf("file changed while copying")
	}
	return nil
}

func copyToPath(src *os.File, srcInfo os.FileInfo, dest string, entry Entry) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".snapshot-file-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := copyAndSetMetadata(src, tmp, srcInfo, entry); err != nil {
		return err
	}
	if err := os.Chtimes(tmpName, entry.ModTime, entry.ModTime); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	keep = true
	return nil
}

func copyAndSetMetadata(src, dst *os.File, srcInfo os.FileInfo, entry Entry) error {
	written, err := io.Copy(dst, src)
	if err != nil {
		return err
	}
	if written != entry.Size || srcInfo.Size() != entry.Size {
		return fmt.Errorf("copied %d bytes, expected %d", written, entry.Size)
	}
	if err := dst.Chmod(os.FileMode(entry.Mode)); err != nil {
		return err
	}
	return dst.Sync()
}

func writeFileAtomic(name string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(name)
	tmp, err := os.CreateTemp(dir, ".snapshot-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, name); err != nil {
		return err
	}
	keep = true
	return nil
}

func sortedEntries(entries []Entry, deepestFirst bool) []Entry {
	out := append([]Entry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		di, dj := entryDepth(out[i]), entryDepth(out[j])
		if di != dj {
			if deepestFirst {
				return di > dj
			}
			return di < dj
		}
		return out[i].Path < out[j].Path
	})
	return out
}
