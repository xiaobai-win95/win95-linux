// Package snapshot provides filesystem snapshots that reuse unchanged files by
// hard-linking them to an earlier snapshot. Snapshot payloads are never
// hard-linked to the live tree, so restoring or editing a live file cannot
// modify a saved snapshot.
package snapshot

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	manifestFormat = 1
	manifestName   = "manifest.json"
	payloadDirName = "files"
	maxManifestLen = 64 << 20
	storableMode   = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
)

// Progress phases reported by Create and Restore.
const (
	PhaseScanning  = "scanning"
	PhaseSkipped   = "skipped"
	PhaseCopying   = "copying"
	PhaseLinking   = "linking"
	PhaseRestoring = "restoring"
	PhaseConflict  = "conflict"
	PhaseDone      = "done"
)

// Progress describes the current snapshot or restore operation. Done and Total
// count manifest entries, including directories and symbolic links. Create's
// Total and BytesTotal grow as entries are discovered and are final at
// PhaseDone; Restore knows both totals before it starts copying.
type Progress struct {
	Phase      string
	Path       string
	Done       int
	Total      int
	BytesDone  int64
	BytesTotal int64
	Reused     int
	Copied     int
}

// EntryType identifies the kind of filesystem object recorded in a manifest.
type EntryType string

const (
	EntryFile      EntryType = "file"
	EntryDirectory EntryType = "directory"
	EntrySymlink   EntryType = "symlink"
)

// Entry is one manifest entry. Path is a slash-separated path relative to the
// snapshot root; "." denotes the root directory itself. Mode contains
// permission and setuid/setgid/sticky bits. Target is populated only for
// symbolic links.
type Entry struct {
	Path    string    `json:"path"`
	Type    EntryType `json:"type"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size,omitempty"`
	Target  string    `json:"target,omitempty"`
}

// Snapshot is the metadata stored in a snapshot's manifest.json. ID is also
// the snapshot directory name beneath baseDir. Payload files are stored under
// <baseDir>/<ID>/files using the manifest's relative paths.
type Snapshot struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Root        string    `json:"root"`
	Entries     []Entry   `json:"entries"`
	Files       int       `json:"files"`
	Directories int       `json:"directories"`
	Symlinks    int       `json:"symlinks"`
	Bytes       int64     `json:"bytes"`
	Reused      int       `json:"reused,omitempty"`
	Copied      int       `json:"copied,omitempty"`
}

// Conflict describes an existing destination object whose type prevents a
// safe restore. Paths are relative to the restore root.
type Conflict struct {
	Path     string `json:"path"`
	Existing string `json:"existing"`
	Required string `json:"required"`
}

// ConflictError is returned before Restore changes the destination when one
// or more destination paths have incompatible types. Changed regular files
// are expected during a rollback and are not conflicts.
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	if len(e.Conflicts) == 1 {
		c := e.Conflicts[0]
		return fmt.Sprintf("snapshot restore conflict at %q: existing %s, snapshot requires %s", c.Path, c.Existing, c.Required)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot restore has %d conflicts:", len(e.Conflicts))
	for _, c := range e.Conflicts {
		fmt.Fprintf(&b, "\n  %q: existing %s, snapshot requires %s", c.Path, c.Existing, c.Required)
	}
	return b.String()
}

type manifest struct {
	Version int `json:"version"`
	Snapshot
}

// List returns valid snapshots in newest-first order. Missing snapshot
// directories, non-directory entries, and incomplete snapshots without a
// manifest are ignored. A malformed manifest is reported as an error.
func List(baseDir string) ([]Snapshot, error) {
	base, err := prepareBaseDir(baseDir, false)
	if errors.Is(err, fs.ErrNotExist) {
		return []Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}

	dirEntries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("read snapshot base: %w", err)
	}
	out := make([]Snapshot, 0)
	for _, de := range dirEntries {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect snapshot entry %q: %w", de.Name(), err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		snap, err := readSnapshot(base, de.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read snapshot %q: %w", de.Name(), err)
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func readSnapshot(base, id string) (Snapshot, error) {
	if !validSnapshotID(id) {
		return Snapshot{}, fmt.Errorf("invalid snapshot ID %q", id)
	}
	dir := filepath.Join(base, id)
	info, err := os.Lstat(dir)
	if err != nil {
		return Snapshot{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, fmt.Errorf("snapshot path is not a real directory")
	}

	manifestPath := filepath.Join(dir, manifestName)
	f, manifestInfo, err := openRegularFile(manifestPath)
	if err != nil {
		return Snapshot{}, err
	}
	defer f.Close()
	if manifestInfo.Size() > maxManifestLen {
		return Snapshot{}, fmt.Errorf("manifest exceeds %d bytes", maxManifestLen)
	}

	dec := json.NewDecoder(io.LimitReader(f, maxManifestLen+1))
	var m manifest
	if err := dec.Decode(&m); err != nil {
		return Snapshot{}, fmt.Errorf("decode %s: %w", manifestName, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Snapshot{}, fmt.Errorf("decode %s: trailing JSON value", manifestName)
		}
		return Snapshot{}, fmt.Errorf("decode %s: %w", manifestName, err)
	}
	if m.Version != manifestFormat {
		return Snapshot{}, fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if m.ID != id {
		return Snapshot{}, fmt.Errorf("manifest ID %q does not match directory %q", m.ID, id)
	}
	if m.CreatedAt.IsZero() || !filepath.IsAbs(m.Root) {
		return Snapshot{}, fmt.Errorf("manifest has invalid creation time or root")
	}
	if err := validateEntries(m.Entries); err != nil {
		return Snapshot{}, err
	}
	return m.Snapshot, nil
}

func validateEntries(entries []Entry) error {
	if len(entries) == 0 {
		return fmt.Errorf("manifest has no entries")
	}
	seen := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if !fs.ValidPath(e.Path) || strings.IndexByte(e.Path, 0) >= 0 || !filepath.IsLocal(filepath.FromSlash(e.Path)) {
			return fmt.Errorf("manifest contains unsafe path %q", e.Path)
		}
		if filepath.Separator == '\\' && strings.ContainsRune(e.Path, '\\') {
			return fmt.Errorf("manifest contains unsafe path %q", e.Path)
		}
		if _, ok := seen[e.Path]; ok {
			return fmt.Errorf("manifest contains duplicate path %q", e.Path)
		}
		if e.Mode & ^uint32(storableMode) != 0 {
			return fmt.Errorf("manifest entry %q has invalid mode %#o", e.Path, e.Mode)
		}
		if e.Path == "." && e.Type != EntryDirectory {
			return fmt.Errorf("manifest root must be a directory")
		}
		switch e.Type {
		case EntryFile:
			if e.Size < 0 || e.Target != "" {
				return fmt.Errorf("manifest file entry %q is invalid", e.Path)
			}
		case EntryDirectory:
			if e.Size != 0 || e.Target != "" {
				return fmt.Errorf("manifest directory entry %q is invalid", e.Path)
			}
		case EntrySymlink:
			if e.Path == "." || e.Target == "" || strings.IndexByte(e.Target, 0) >= 0 {
				return fmt.Errorf("manifest symlink entry %q is invalid", e.Path)
			}
		default:
			return fmt.Errorf("manifest entry %q has unknown type %q", e.Path, e.Type)
		}
		seen[e.Path] = e
	}
	if root, ok := seen["."]; !ok || root.Type != EntryDirectory {
		return fmt.Errorf("manifest has no root directory entry")
	}
	for _, e := range entries {
		if e.Path == "." {
			continue
		}
		parent := path.Dir(e.Path)
		if parent == "." {
			continue
		}
		p, ok := seen[parent]
		if !ok || p.Type != EntryDirectory {
			return fmt.Errorf("manifest entry %q has a missing or non-directory parent", e.Path)
		}
	}
	return nil
}

func validSnapshotID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.HasPrefix(id, ".") {
		return false
	}
	if strings.ContainsAny(id, "/\\\x00") {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func prepareRootDir(root string, create bool) (string, os.FileInfo, error) {
	if root == "" {
		return "", nil, fmt.Errorf("root directory is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve root: %w", err)
	}
	if create {
		if err := os.MkdirAll(abs, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", nil, fmt.Errorf("create root: %w", err)
		}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", nil, fmt.Errorf("root %q is not a real directory", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, fmt.Errorf("resolve root symlinks: %w", err)
	}
	info, err = os.Lstat(resolved)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", nil, fmt.Errorf("resolved root %q is not a real directory", resolved)
	}
	return filepath.Clean(resolved), info, nil
}

func prepareBaseDir(baseDir string, create bool) (string, error) {
	if baseDir == "" {
		return "", fmt.Errorf("snapshot base directory is empty")
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve snapshot base: %w", err)
	}
	if create {
		if err := os.MkdirAll(abs, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("create snapshot base: %w", err)
		}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("snapshot base %q is not a real directory", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve snapshot base symlinks: %w", err)
	}
	info, err = os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("resolved snapshot base %q is not a real directory", resolved)
	}
	return filepath.Clean(resolved), nil
}

func pathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func excludedRootPath(rel string) bool {
	if rel == "." {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	switch first {
	case "proc", "sys", "dev", "run", "tmp":
		return true
	default:
		return false
	}
}

func openRegularFile(name string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%q is not a regular file", name)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		f.Close()
		return nil, nil, fmt.Errorf("%q changed while opening", name)
	}
	return f, info, nil
}

func openDirectoryRoot(dir string) (*os.Root, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%q is not a real directory", dir)
	}
	return openDirectoryRootWithInfo(dir, info)
}

// openDirectoryRootWithInfo verifies the identity of the opened directory, not
// just the path that was checked earlier. On Unix the returned Root retains the
// directory fd, so later path replacement or renaming cannot redirect writes.
func openDirectoryRootWithInfo(dir string, expected os.FileInfo) (*os.Root, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	openedInfo, err := r.Lstat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(expected, openedInfo) {
		r.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%q changed while opening", dir)
	}
	return r, nil
}

// lstatNoSymlinkParents checks every parent before asking Root to resolve the
// final component. Root provides confinement; these checks ensure a manifest
// path never traverses a symbolic-link directory in a static snapshot tree.
func lstatNoSymlinkParents(r *os.Root, rel string) (os.FileInfo, error) {
	if rel == "." {
		return r.Lstat(".")
	}
	parent := path.Dir(rel)
	if parent != "." {
		current := ""
		for _, part := range strings.Split(parent, "/") {
			current = path.Join(current, part)
			info, err := r.Lstat(filepath.FromSlash(current))
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("parent %q of %q is not a real directory", current, rel)
			}
		}
	}
	return r.Lstat(filepath.FromSlash(rel))
}

func metadataMatches(info os.FileInfo, e Entry) bool {
	return info.Mode()&storableMode == os.FileMode(e.Mode) && info.ModTime().Equal(e.ModTime)
}

func fileMetadataMatches(info os.FileInfo, e Entry) bool {
	return info.Mode().IsRegular() && info.Size() == e.Size && metadataMatches(info, e)
}

func randomSuffix(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func report(progress func(Progress), p Progress) {
	if progress != nil {
		progress(p)
	}
}

func objectType(info os.FileInfo) string {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "regular file"
	default:
		return "special file"
	}
}

func entryDepth(e Entry) int {
	if e.Path == "." {
		return 0
	}
	return strings.Count(e.Path, "/") + 1
}
