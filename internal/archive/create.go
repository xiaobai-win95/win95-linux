package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

type createItem struct {
	name   string
	source string
	info   fs.FileInfo
	entry  Entry
}

func createArchive(dest string, paths []string, format string, limits Limits, progress ProgressFunc) error {
	limits, err := limits.normalized()
	if err != nil {
		return err
	}
	outputFormat, err := normalizeCreateFormat(dest, format)
	if err != nil {
		return err
	}
	if dest == "" {
		return fmt.Errorf("archive: empty destination: %w", ErrUnsafePath)
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("archive: resolve destination: %w", err)
	}
	if info, err := os.Lstat(destAbs); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("archive: destination is a symlink: %w", ErrUnsafePath)
		}
		if info.IsDir() {
			return fmt.Errorf("archive: destination is a directory: %w", ErrUnsafePath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("archive: inspect destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return fmt.Errorf("archive: create destination directory: %w", err)
	}
	canonicalParent, err := filepath.EvalSymlinks(filepath.Dir(destAbs))
	if err != nil {
		return fmt.Errorf("archive: resolve destination directory: %w", err)
	}
	destAbs = filepath.Join(canonicalParent, filepath.Base(destAbs))

	items, total, err := planCreation(destAbs, paths, limits)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destAbs), ".archive-*")
	if err != nil {
		return fmt.Errorf("archive: create temporary output: %w", err)
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	state := newProgressState(progress, total)
	bounded := &boundedWriter{w: tmp, remaining: limits.MaxArchiveBytes}
	if err := writeCreatedArchive(bounded, outputFormat, items, limits, state); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("archive: sync output: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("archive: close output: %w", err)
	}
	if err := os.Rename(tmpName, destAbs); err != nil {
		return fmt.Errorf("archive: replace destination: %w", err)
	}
	renamed = true
	state.end()
	return nil
}

func planCreation(dest string, paths []string, limits Limits) ([]createItem, int64, error) {
	items := make([]createItem, 0)
	entries := make([]Entry, 0)
	var total int64
	for _, source := range paths {
		if source == "" {
			return nil, 0, fmt.Errorf("archive: empty source path: %w", ErrUnsafePath)
		}
		sourceAbs, err := filepath.Abs(source)
		if err != nil {
			return nil, 0, fmt.Errorf("archive: resolve source %q: %w", source, err)
		}
		originalInfo, err := os.Lstat(sourceAbs)
		if err != nil {
			return nil, 0, fmt.Errorf("archive: inspect source %q: %w", source, err)
		}
		if originalInfo.Mode()&fs.ModeSymlink != 0 {
			return nil, 0, fmt.Errorf("archive: source %q is a symlink: %w", source, ErrUnsupportedType)
		}
		canonical, err := filepath.EvalSymlinks(sourceAbs)
		if err != nil {
			return nil, 0, fmt.Errorf("archive: resolve source %q: %w", source, err)
		}
		info, err := os.Lstat(canonical)
		if err != nil {
			return nil, 0, fmt.Errorf("archive: inspect source %q: %w", source, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return nil, 0, fmt.Errorf("archive: source %q is a link or special file: %w", source, ErrUnsupportedType)
		}
		if canonical == dest || (info.IsDir() && pathWithin(canonical, dest)) {
			return nil, 0, fmt.Errorf("archive: destination is inside source %q: %w", source, ErrUnsafePath)
		}
		base := filepath.Base(canonical)
		err = filepath.WalkDir(canonical, func(current string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == canonical && d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("archive: source %q is a symlink: %w", source, ErrUnsupportedType)
			}
			entryInfo, err := d.Info()
			if err != nil {
				return err
			}
			if entryInfo.Mode()&fs.ModeSymlink != 0 || (!entryInfo.IsDir() && !entryInfo.Mode().IsRegular()) {
				return fmt.Errorf("archive: source %q is a link or special file: %w", current, ErrUnsupportedType)
			}
			rel, err := filepath.Rel(canonical, current)
			if err != nil {
				return err
			}
			name := base
			if rel != "." {
				name = filepath.Join(base, rel)
			}
			name = filepath.ToSlash(name)
			normalized, root, err := normalizeName(name, limits)
			if err != nil {
				return err
			}
			if root {
				return fmt.Errorf("archive: invalid source root %q: %w", source, ErrUnsafePath)
			}
			entry := Entry{Name: normalized, ModTime: entryInfo.ModTime(), Mode: entryInfo.Mode(), Type: entryType(entryInfo.Mode())}
			if entry.Type == TypeDir {
				entry.Size = 0
			} else {
				entry.Size = entryInfo.Size()
				if entry.Size > limits.MaxTotalBytes-total {
					return fmt.Errorf("archive: total source size exceeds limit: %w", ErrLimitExceeded)
				}
				total += entry.Size
			}
			entry, err = validateEntry(entry, limits)
			if err != nil {
				return err
			}
			if len(entries) >= limits.MaxEntries {
				return fmt.Errorf("archive: too many source entries: %w", ErrLimitExceeded)
			}
			entries = append(entries, entry)
			items = append(items, createItem{name: entry.Name, source: current, info: entryInfo, entry: entry})
			return nil
		})
		if err != nil {
			return nil, 0, fmt.Errorf("archive: plan source %q: %w", source, err)
		}
	}
	if err := validatePaths(entries, limits); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type boundedWriter struct {
	w         io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("archive: output exceeds size limit: %w", ErrLimitExceeded)
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func writeCreatedArchive(output io.Writer, format Format, items []createItem, limits Limits, state *progressState) (err error) {
	var closers []io.Closer
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closeErr := closers[i].Close()
			if err == nil && closeErr != nil {
				err = fmt.Errorf("archive: close created archive: %w", closeErr)
			}
		}
	}()

	if format == FormatZIP {
		zw := zip.NewWriter(output)
		closers = append(closers, zw)
		for _, item := range items {
			if err := writeZIPItem(zw, item, limits, state); err != nil {
				return err
			}
		}
		return nil
	}

	compressed, err := newTarCompressor(output, format, limits)
	if err != nil {
		return err
	}
	closers = append(closers, compressed)
	tw := tar.NewWriter(compressed)
	closers = append(closers, tw)
	for _, item := range items {
		if err := writeTarItem(tw, item, limits, state); err != nil {
			return err
		}
	}
	return nil
}

func newTarCompressor(output io.Writer, format Format, limits Limits) (io.WriteCloser, error) {
	switch format {
	case FormatTar:
		return nopWriteCloser{Writer: output}, nil
	case FormatTarGZ:
		return gzip.NewWriter(output), nil
	case FormatTarXZ:
		dict := limits.MaxDecoderMemory
		if dict > 8<<20 {
			dict = 8 << 20
		}
		cfg := xz.WriterConfig{DictCap: int(dict), CheckSum: xz.CRC64}
		return cfg.NewWriter(output)
	case FormatTarZST:
		window := limits.MaxDecoderMemory
		if window > 1<<20 {
			window = 1 << 20
		}
		return zstd.NewWriter(output,
			zstd.WithEncoderConcurrency(1),
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithWindowSize(int(window)),
			zstd.WithLowerEncoderMem(true),
		)
	default:
		return nil, fmt.Errorf("archive: cannot create %q: %w", format, ErrUnsupportedFormat)
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func writeZIPItem(zw *zip.Writer, item createItem, limits Limits, state *progressState) error {
	header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
	if item.entry.Type == TypeDir {
		header.Name += "/"
		header.Method = zip.Store
	}
	header.SetMode(item.info.Mode())
	header.SetModTime(item.info.ModTime())
	if item.entry.Type == TypeDir {
		if err := verifyDirectory(item); err != nil {
			return err
		}
		_, err := zw.CreateHeader(header)
		return err
	}
	src, err := openPlannedFile(item)
	if err != nil {
		return err
	}
	defer src.Close()
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	return copyPlannedFile(writer, src, item, limits, state)
}

func writeTarItem(tw *tar.Writer, item createItem, limits Limits, state *progressState) error {
	header, err := tar.FileInfoHeader(item.info, "")
	if err != nil {
		return fmt.Errorf("archive: build tar header for %q: %w", item.name, err)
	}
	header.Name = item.name
	if item.entry.Type == TypeDir {
		header.Name += "/"
	}
	header.Format = tar.FormatPAX
	if item.entry.Type == TypeDir {
		if err := verifyDirectory(item); err != nil {
			return err
		}
		return tw.WriteHeader(header)
	}
	src, err := openPlannedFile(item)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	return copyPlannedFile(tw, src, item, limits, state)
}

func verifyDirectory(item createItem) error {
	info, err := os.Lstat(item.source)
	if err != nil {
		return fmt.Errorf("archive: source directory changed: %w", err)
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("archive: source directory %q changed: %w", item.source, ErrInvalidArchive)
	}
	return nil
}

func openPlannedFile(item createItem) (*os.File, error) {
	f, err := os.Open(item.source)
	if err != nil {
		return nil, fmt.Errorf("archive: open source %q: %w", item.source, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("archive: stat source %q: %w", item.source, err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, item.info) || info.Size() != item.info.Size() {
		f.Close()
		return nil, fmt.Errorf("archive: source file %q changed while creating archive: %w", item.source, ErrInvalidArchive)
	}
	return f, nil
}

func copyPlannedFile(dst io.Writer, src io.Reader, item createItem, limits Limits, state *progressState) error {
	state.start(item.name, item.entry.Size)
	writer := &progressWriter{dst: dst, state: state, name: item.name}
	n, err := copyExactly(writer, src, item.entry.Size, limits.MaxFileBytes, limits.MaxTotalBytes-state.done)
	if err != nil {
		return fmt.Errorf("archive: copy source %q: %w", item.source, err)
	}
	if n != item.entry.Size {
		return fmt.Errorf("archive: source file %q changed while creating archive: %w", item.source, ErrInvalidArchive)
	}
	state.finish(item.name)
	return nil
}
