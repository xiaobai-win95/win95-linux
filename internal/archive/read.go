package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

type archiveItem struct {
	entry   Entry
	open    func() (io.ReadCloser, error)
	consume func(io.Writer) (int64, error)
}

type itemVisitor func(*archiveItem) error

func listArchive(f *os.File, format Format, limits Limits) ([]Entry, error) {
	var entries []Entry
	err := walkArchive(f, format, limits, false, nil, func(item *archiveItem) error {
		entries = append(entries, item.entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// walkArchive validates every member on every pass. expectedSizes is populated
// by extraction preflight for RAR members whose headers omit an unpacked size.
func walkArchive(f *os.File, format Format, limits Limits, extracting bool, expectedSizes map[string]int64, visit itemVisitor) error {
	var err error
	switch format {
	case FormatZIP:
		err = walkZIP(f, limits, extracting, visit)
	case Format7z:
		err = walk7z(f, limits, extracting, visit)
	case FormatRAR:
		err = walkRAR(f, limits, extracting, expectedSizes, visit)
	case FormatTar, FormatTarGZ, FormatTarXZ, FormatTarZST:
		err = walkTar(f, format, limits, extracting, visit)
	default:
		return fmt.Errorf("archive: cannot read format %q: %w", format, ErrUnsupportedFormat)
	}
	if err != nil {
		return err
	}
	return nil
}

func walkZIP(f *os.File, limits Limits, extracting bool, visit itemVisitor) error {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	zr, err := zip.NewReader(io.NewSectionReader(f, 0, size), size)
	if err != nil {
		return fmt.Errorf("archive: invalid ZIP: %w: %w", ErrInvalidArchive, err)
	}
	if len(zr.File) > limits.MaxEntries {
		return fmt.Errorf("archive: too many ZIP entries: %w", ErrLimitExceeded)
	}
	budget := &copyBudget{remaining: limits.MaxTotalBytes}
	members := make([]Entry, 0, len(zr.File))
	for _, zf := range zr.File {
		if zf.UncompressedSize64 > math.MaxInt64 {
			return fmt.Errorf("archive: ZIP member is too large: %w", ErrLimitExceeded)
		}
		mode := zf.Mode()
		entry := Entry{
			Name:      zf.Name,
			Size:      int64(zf.UncompressedSize64),
			ModTime:   archiveModTime(zf.Modified),
			Mode:      mode,
			Type:      entryType(mode),
			Encrypted: zf.Flags&1 != 0,
		}
		item, root, err := prepareItem(entry, limits, extracting)
		if err != nil {
			return err
		}
		if root {
			continue
		}
		file := zf
		item.open = func() (io.ReadCloser, error) {
			rc, err := file.Open()
			if err != nil {
				return nil, archiveReadError("ZIP", file.Name, err)
			}
			return rc, nil
		}
		if item.entry.Type == TypeSymlink {
			target, err := readLinkTarget("ZIP", file.Name, item.open, item.entry.Size, limits, budget)
			if err != nil {
				return err
			}
			item.entry.LinkTarget = target
		}
		members = append(members, item.entry)
		if err := consumeItem(item, limits, extracting, budget, visit); err != nil {
			return err
		}
	}
	return validatePaths(members, limits)
}

func walk7z(f *os.File, limits Limits, extracting bool, visit itemVisitor) error {
	limits, err := limits.normalized()
	if err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Registration and the active limit are process-wide. Serialize 7z walks so
	// one archive can never relax another archive's decoder budget.
	sevenzipWalkMu.Lock()
	defer sevenzipWalkMu.Unlock()
	sevenzipMemoryLimit.Store(limits.MaxDecoderMemory)
	defer sevenzipMemoryLimit.Store(0)
	if err := preflight7z(f, size, limits); err != nil {
		return err
	}
	zr, err := sevenzip.NewReader(io.NewSectionReader(f, 0, size), size)
	if err != nil {
		return fmt.Errorf("archive: invalid 7z: %w: %w", ErrInvalidArchive, err)
	}
	if len(zr.File) > limits.MaxEntries {
		return fmt.Errorf("archive: too many 7z entries: %w", ErrLimitExceeded)
	}
	budget := &copyBudget{remaining: limits.MaxTotalBytes}
	members := make([]Entry, 0, len(zr.File))
	for _, zf := range zr.File {
		if zf.UncompressedSize > math.MaxInt64 {
			return fmt.Errorf("archive: 7z member is too large: %w", ErrLimitExceeded)
		}
		mode := zf.Mode()
		entry := Entry{
			Name:    zf.Name,
			Size:    int64(zf.UncompressedSize),
			ModTime: archiveModTime(zf.Modified),
			Mode:    mode,
			Type:    entryType(mode),
		}
		item, root, err := prepareItem(entry, limits, extracting)
		if err != nil {
			return err
		}
		if root {
			continue
		}
		file := zf
		item.open = func() (io.ReadCloser, error) {
			rc, err := file.Open()
			if err != nil {
				return nil, archiveReadError("7z", file.Name, err)
			}
			return rc, nil
		}
		if item.entry.Type == TypeSymlink {
			target, err := readLinkTarget("7z", file.Name, item.open, item.entry.Size, limits, budget)
			if err != nil {
				return err
			}
			item.entry.LinkTarget = target
		}
		members = append(members, item.entry)
		if err := consumeItem(item, limits, extracting, budget, visit); err != nil {
			return err
		}
	}
	return validatePaths(members, limits)
}

func walkRAR(f *os.File, limits Limits, extracting bool, expectedSizes map[string]int64, visit itemVisitor) error {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	rr, err := rardecode.NewReader(newBoundedReader(f, size), rardecode.MaxDictionarySize(limits.MaxDecoderMemory))
	if err != nil {
		return rarReadError("open RAR", err)
	}
	budget := &copyBudget{remaining: limits.MaxTotalBytes}
	count := 0
	members := make([]Entry, 0)
	for {
		h, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rarReadError("read RAR header", err)
		}
		count++
		if count > limits.MaxEntries {
			return fmt.Errorf("archive: too many RAR entries: %w", ErrLimitExceeded)
		}
		mode := h.Mode()
		typ := entryType(mode)
		switch h.LinkType {
		case rardecode.LinkTypeUnixSymlink, rardecode.LinkTypeWindowsSymlink, rardecode.LinkTypeWindowsJunction:
			typ = TypeSymlink
		case rardecode.LinkTypeHardLink:
			typ = TypeHardLink
		}
		declared := h.UnPackedSize
		unknown := h.UnKnownSize || declared < 0
		if unknown {
			declared = 0
		}
		entry := Entry{
			Name:       h.Name,
			Size:       declared,
			ModTime:    archiveModTime(h.ModificationTime),
			Mode:       mode,
			Type:       typ,
			LinkTarget: h.LinkTarget,
			Encrypted:  h.Encrypted || h.HeaderEncrypted,
		}
		item, root, err := prepareItem(entry, limits, extracting)
		if err != nil {
			return err
		}
		if unknown && extracting {
			if expected, ok := expectedSizes[item.entry.Name]; ok {
				item.entry.Size, unknown = expected, false
				declared = expected
			}
		}
		if !root {
			members = append(members, item.entry)
		}

		consume := func(dst io.Writer) (int64, error) {
			if h.IsDir || typ == TypeDir {
				return 0, nil
			}
			var n int64
			if unknown {
				limit := limits.MaxFileBytes
				if budget.remaining < limit {
					limit = budget.remaining
				}
				var err error
				n, err = copyStream(dst, rr, limit)
				if err != nil {
					return n, rarReadError("read RAR member "+h.Name, err)
				}
				item.entry.Size = n
			} else {
				var err error
				n, err = copyExactly(dst, rr, declared, limits.MaxFileBytes, budget.remaining)
				if err != nil {
					return n, rarReadError("read RAR member "+h.Name, err)
				}
			}
			if err := budget.reserve(n); err != nil {
				return n, err
			}
			return n, nil
		}

		if extracting && !root {
			if err := visitWithReader(&item, consume, visit); err != nil {
				return err
			}
		} else if !root {
			if _, err := consume(io.Discard); err != nil {
				return err
			}
			if err := visit(&item); err != nil {
				return err
			}
		} else if _, err := consume(io.Discard); err != nil {
			return err
		}
	}
	return validatePaths(members, limits)
}

func walkTar(f *os.File, format Format, limits Limits, extracting bool, visit itemVisitor) error {
	stream, err := openDecompressor(f, format, limits)
	if err != nil {
		return err
	}
	defer stream.Close()
	limited := &cappedReader{r: stream, remaining: limits.MaxTotalBytes}
	tr := tar.NewReader(limited)
	budget := &copyBudget{remaining: limits.MaxTotalBytes}
	count := 0
	members := make([]Entry, 0)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("archive: invalid tar: %w: %w", ErrInvalidArchive, err)
		}
		count++
		if count > limits.MaxEntries {
			return fmt.Errorf("archive: too many tar entries: %w", ErrLimitExceeded)
		}
		typ := TypeOther
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			typ = TypeFile
		case tar.TypeDir:
			typ = TypeDir
		case tar.TypeSymlink:
			typ = TypeSymlink
		case tar.TypeLink:
			typ = TypeHardLink
		}
		entry := Entry{
			Name:       h.Name,
			Size:       h.Size,
			ModTime:    archiveModTime(h.ModTime),
			Mode:       h.FileInfo().Mode(),
			Type:       typ,
			LinkTarget: h.Linkname,
		}
		item, root, err := prepareItem(entry, limits, extracting)
		if err != nil {
			return err
		}
		if root {
			continue
		}
		members = append(members, item.entry)
		file := item
		file.open = func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
		if err := consumeItem(file, limits, extracting, budget, visit); err != nil {
			return err
		}
	}
	// tar.Reader stops at its end marker. Drain the decompressor to verify
	// gzip/XZ/Zstandard checksums and reject oversized trailing data.
	if _, err := copyStream(nil, limited, limited.remaining); err != nil {
		return fmt.Errorf("archive: invalid compressed tar trailer: %w: %w", ErrInvalidArchive, err)
	}
	return validatePaths(members, limits)
}

func prepareItem(entry Entry, limits Limits, extracting bool) (archiveItem, bool, error) {
	entry, err := validateEntry(entry, limits)
	if err != nil {
		return archiveItem{}, false, err
	}
	if extracting {
		if err := validateExtractable([]Entry{entry}); err != nil {
			return archiveItem{}, false, err
		}
	}
	return archiveItem{entry: entry}, entry.Name == ".", nil
}

func archiveModTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(time.Local)
}

// readLinkTarget reads link metadata without ever materializing the link. Link
// payloads count against the same streaming budget as regular file contents.
func readLinkTarget(context, name string, open func() (io.ReadCloser, error), size int64, limits Limits, budget *copyBudget) (string, error) {
	if size > int64(limits.MaxPathBytes) {
		return "", fmt.Errorf("archive: link target in %q is too large: %w", name, ErrLimitExceeded)
	}
	rc, err := open()
	if err != nil {
		return "", err
	}
	var target bytes.Buffer
	n, copyErr := copyExactly(&target, rc, size, limits.MaxFileBytes, budget.remaining)
	closeErr := rc.Close()
	if copyErr != nil {
		return "", archiveReadError(context, name, copyErr)
	}
	if closeErr != nil {
		return "", archiveReadError(context, name, closeErr)
	}
	if err := budget.reserve(n); err != nil {
		return "", err
	}
	value := target.String()
	if n != size || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("archive: invalid link target in %q: %w", name, ErrInvalidArchive)
	}
	return value, nil
}

func consumeItem(item archiveItem, limits Limits, extracting bool, budget *copyBudget, visit itemVisitor) error {
	if item.entry.Type != TypeFile {
		return visit(&item)
	}
	if extracting {
		return visit(&item)
	}
	rc, err := item.open()
	if err != nil {
		return err
	}
	n, copyErr := copyExactly(io.Discard, rc, item.entry.Size, limits.MaxFileBytes, budget.remaining)
	closeErr := rc.Close()
	if copyErr != nil {
		return archiveReadError("archive", item.entry.Name, copyErr)
	}
	if closeErr != nil {
		return archiveReadError("archive", item.entry.Name, closeErr)
	}
	if err := budget.reserve(n); err != nil {
		return err
	}
	return visit(&item)
}

func visitWithReader(item *archiveItem, consume func(io.Writer) (int64, error), visit itemVisitor) error {
	if item.entry.Type != TypeFile {
		return visit(item)
	}
	item.consume = consume
	return visit(item)
}

func archiveReadError(context, name string, err error) error {
	var re sevenzip.ReadError
	var rep *sevenzip.ReadError
	if errors.As(err, &re) || errors.As(err, &rep) {
		if re.Encrypted || (rep != nil && rep.Encrypted) {
			return fmt.Errorf("archive: encrypted %s member %q: %w", context, name, ErrEncrypted)
		}
	}
	return fmt.Errorf("archive: invalid %s member %q: %w: %w", context, name, ErrInvalidArchive, err)
}

func rarReadError(operation string, err error) error {
	if errors.Is(err, rardecode.ErrArchiveEncrypted) || errors.Is(err, rardecode.ErrArchivedFileEncrypted) {
		return fmt.Errorf("archive: %s: %w", operation, ErrEncrypted)
	}
	if errors.Is(err, rardecode.ErrDictionaryTooLarge) {
		return fmt.Errorf("archive: %s: decoder dictionary exceeds limit: %w", operation, ErrLimitExceeded)
	}
	if errors.Is(err, rardecode.ErrMultiVolume) {
		return fmt.Errorf("archive: %s: multi-volume RAR is unsupported: %w", operation, ErrUnsupportedFormat)
	}
	return fmt.Errorf("archive: %s: %w: %w", operation, ErrInvalidArchive, err)
}
