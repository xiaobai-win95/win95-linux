package archive

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func openDestination(dest string) (*os.Root, error) {
	if dest == "" {
		return nil, fmt.Errorf("archive: empty destination: %w", ErrUnsafePath)
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, fmt.Errorf("archive: resolve destination: %w", err)
	}
	clean := filepath.Clean(abs)
	if info, statErr := os.Lstat(clean); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("archive: destination is a symbolic link: %w", ErrUnsafePath)
	}
	existing := clean
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(existing)
		if resolveErr == nil {
			clean = resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				clean = filepath.Join(clean, suffix[i])
			}
			break
		}
		if !errors.Is(resolveErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("archive: resolve destination %q: %w", dest, resolveErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return nil, fmt.Errorf("archive: resolve destination %q: %w", dest, resolveErr)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, fmt.Errorf("archive: open destination base: %w", err)
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next, err := openDirectory(root, part, true)
		if err != nil {
			root.Close()
			return nil, fmt.Errorf("archive: open destination %q: %w", dest, err)
		}
		root.Close()
		root = next
	}
	return root, nil
}

func openDirectory(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && create {
		if mkdirErr := parent.Mkdir(name, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
			return nil, mkdirErr
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("path component %q is not a real directory: %w", name, ErrUnsafePath)
	}
	next, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return next, nil
}

type directoryMetadata struct {
	entry Entry
	depth int
}

func extractFiles(root *os.Root, f *os.File, format Format, limits Limits, entries []Entry, state *progressState) error {
	expected := make(map[string]int64, len(entries))
	for _, entry := range entries {
		if entry.Type == TypeFile {
			expected[entry.Name] = entry.Size
		}
	}
	directories := make(map[string]directoryMetadata)
	visitor := func(item *archiveItem) error {
		entry := item.entry
		if entry.Name == "." {
			return nil
		}
		if entry.Type == TypeDir {
			if err := ensureDirectories(root, entry.Name); err != nil {
				return err
			}
			directories[entry.Name] = directoryMetadata{entry: entry, depth: strings.Count(entry.Name, "/") + 1}
			return nil
		}
		if err := ensureDirectories(root, path.Dir(entry.Name)); err != nil {
			return err
		}
		if err := extractMember(root, item, limits, state); err != nil {
			return err
		}
		return nil
	}
	if err := walkArchive(f, format, limits, true, expected, visitor); err != nil {
		return err
	}

	ordered := make([]directoryMetadata, 0, len(directories))
	for _, dir := range directories {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].depth != ordered[j].depth {
			return ordered[i].depth > ordered[j].depth
		}
		return ordered[i].entry.Name > ordered[j].entry.Name
	})
	for _, dir := range ordered {
		if err := restoreMetadata(root, dir.entry); err != nil {
			return err
		}
	}
	state.end()
	return nil
}

func extractMember(root *os.Root, item *archiveItem, limits Limits, state *progressState) error {
	entry := item.entry
	parent, err := secureParent(root, path.Dir(entry.Name), false)
	if err != nil {
		return fmt.Errorf("archive: open parent of %q: %w", entry.Name, err)
	}
	defer parent.Close()
	name := path.Base(entry.Name)

	var rc io.ReadCloser
	if item.consume == nil {
		rc, err = item.open()
		if err != nil {
			return err
		}
	}
	inputClosed := false
	defer func() {
		if rc != nil && !inputClosed {
			rc.Close()
		}
	}()

	before, err := parent.Lstat(name)
	if err == nil {
		if before.IsDir() {
			return fmt.Errorf("archive: cannot replace directory %q with a file: %w", entry.Name, ErrDuplicatePath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("archive: inspect existing file %q: %w", entry.Name, err)
	}

	token, err := extractionToken()
	if err != nil {
		return fmt.Errorf("archive: create temporary name for %q: %w", entry.Name, err)
	}
	tempName := ".archive-" + token + ".tmp"
	backupName := ".archive-" + token + ".bak"
	out, err := openFileAt(parent, tempName)
	if err != nil {
		return fmt.Errorf("archive: create temporary file for %q: %w", entry.Name, err)
	}
	closed := false
	installed := false
	defer func() {
		if !closed {
			out.Close()
		}
		if !installed {
			parent.Remove(tempName)
			if before != nil {
				parent.Remove(backupName)
			}
		}
	}()

	if before != nil {
		if err := parent.Link(name, backupName); err != nil {
			return fmt.Errorf("archive: create rollback copy for %q: %w", entry.Name, err)
		}
	}

	state.start(entry.Name, entry.Size)
	writer := &progressWriter{dst: out, state: state, name: entry.Name}
	totalLeft := limits.MaxTotalBytes - state.done
	var written int64
	var copyErr error
	if item.consume != nil {
		written, copyErr = item.consume(writer)
	} else {
		written, copyErr = copyExactly(writer, rc, entry.Size, limits.MaxFileBytes, totalLeft)
	}
	if rc != nil {
		inputClosed = true
		if closeErr := rc.Close(); copyErr == nil {
			copyErr = closeErr
		}
		rc = nil
	}
	if copyErr != nil {
		return archiveReadError("archive", entry.Name, copyErr)
	}
	if written != entry.Size {
		return fmt.Errorf("archive: member %q has an unexpected size: %w", entry.Name, ErrInvalidArchive)
	}
	if err := restoreOpenedFileMetadata(out, entry); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("archive: sync file %q: %w", entry.Name, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("archive: close file %q: %w", entry.Name, err)
	}
	closed = true

	// Do not overwrite a path that changed while the temporary file was written.
	current, err := parent.Lstat(name)
	if before == nil {
		if err == nil {
			return fmt.Errorf("archive: destination %q appeared during extraction: %w", entry.Name, ErrUnsafePath)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("archive: recheck destination %q: %w", entry.Name, err)
		}
	} else {
		if err != nil {
			return fmt.Errorf("archive: recheck destination %q: %w", entry.Name, err)
		}
		backup, backupErr := parent.Lstat(backupName)
		if backupErr != nil {
			return fmt.Errorf("archive: inspect rollback copy for %q: %w", entry.Name, backupErr)
		}
		if !os.SameFile(current, before) || !os.SameFile(backup, before) {
			return fmt.Errorf("archive: destination %q changed during extraction: %w", entry.Name, ErrUnsafePath)
		}
	}
	if err := parent.Rename(tempName, name); err != nil {
		return fmt.Errorf("archive: replace file %q: %w", entry.Name, err)
	}
	installed = true
	if before != nil {
		if err := parent.Remove(backupName); err != nil {
			return fmt.Errorf("archive: remove rollback copy for %q: %w", entry.Name, err)
		}
	}
	state.finish(entry.Name)
	return nil
}

func extractionToken() (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func openFileAt(parent *os.Root, name string) (*os.File, error) {
	return parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func ensureDirectories(root *os.Root, name string) error {
	if name == "." || name == "" {
		return nil
	}
	dir, err := secureParent(root, name, true)
	if err != nil {
		return fmt.Errorf("archive: open destination directory %q: %w", name, err)
	}
	return dir.Close()
}

func secureParent(root *os.Root, name string, create bool) (*os.Root, error) {
	if name == "." || name == "" {
		return root.OpenRoot(".")
	}
	current := root
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." {
			continue
		}
		next, err := openDirectory(current, part, create)
		if err != nil {
			if current != root {
				current.Close()
			}
			return nil, err
		}
		if current != root {
			current.Close()
		}
		current = next
	}
	return current, nil
}

func restoreMetadata(root *os.Root, entry Entry) error {
	dir, err := secureParent(root, entry.Name, false)
	if err != nil {
		return fmt.Errorf("archive: open directory %q for metadata: %w", entry.Name, err)
	}
	defer dir.Close()
	file, err := dir.Open(".")
	if err != nil {
		return fmt.Errorf("archive: open directory %q for metadata: %w", entry.Name, err)
	}
	defer file.Close()
	return restoreOpenedFileMetadata(file, entry)
}

func restoreOpenedFileMetadata(file *os.File, entry Entry) error {
	if entry.Mode.Perm() != 0 {
		if err := file.Chmod(entry.Mode.Perm()); err != nil {
			return fmt.Errorf("archive: set permissions on %q: %w", entry.Name, err)
		}
	}
	if !entry.ModTime.IsZero() {
		fdPath := "/dev/fd/" + strconv.Itoa(int(file.Fd()))
		if err := os.Chtimes(fdPath, entry.ModTime, entry.ModTime); err != nil {
			return fmt.Errorf("archive: set timestamps on %q: %w", entry.Name, err)
		}
	}
	return nil
}

type progressWriter struct {
	dst   io.Writer
	state *progressState
	name  string
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 {
		w.state.advance(int64(n), w.name)
	}
	return n, err
}
