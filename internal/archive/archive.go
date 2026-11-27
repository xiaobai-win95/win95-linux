// Package archive provides streaming, content-addressed archive detection,
// listing, extraction, and creation for a desktop application.
//
// Extraction is intentionally stricter than listing: links, devices, and other
// special entries are listed when safe but are never materialized on disk.
package archive

import (
	"errors"
	"io/fs"
	"time"
)

// Format identifies an archive format. Detection is based on file contents,
// not on the file name.
type Format string

const (
	FormatUnknown Format = ""
	FormatZIP     Format = "zip"
	Format7z      Format = "7z"
	FormatRAR     Format = "rar"
	FormatTar     Format = "tar"
	FormatTarGZ   Format = "tar.gz"
	FormatTarXZ   Format = "tar.xz"
	FormatTarZST  Format = "tar.zst"
)

// EntryType describes the kind of an archive member.
type EntryType uint8

const (
	TypeFile EntryType = iota
	TypeDir
	TypeSymlink
	TypeHardLink
	TypeOther
)

// Entry is the safe, normalized metadata for one archive member.
type Entry struct {
	Name       string
	Size       int64
	ModTime    time.Time
	Mode       fs.FileMode
	Type       EntryType
	LinkTarget string
	Encrypted  bool
}

// ProgressFunc reports uncompressed bytes processed. total may be zero when an
// archive does not expose a total size. current is empty at the start and end.
type ProgressFunc = func(done, total int64, current string)

var (
	ErrUnsupportedFormat = errors.New("archive: unsupported format")
	ErrUnsafePath        = errors.New("archive: unsafe path")
	ErrLimitExceeded     = errors.New("archive: limit exceeded")
	ErrEncrypted         = errors.New("archive: encrypted entry is not supported")
	ErrLinksUnsupported  = errors.New("archive: links are not supported during extraction")
	ErrDuplicatePath     = errors.New("archive: duplicate or conflicting path")
	ErrUnsupportedType   = errors.New("archive: unsupported entry type")
	ErrInvalidArchive    = errors.New("archive: invalid or corrupt archive")
)

// Identify detects the format of path without extracting it.
func Identify(path string) (Format, error) {
	return IdentifyWithLimits(path, DefaultLimits())
}

// IdentifyWithLimits detects the format while enforcing input-size limits.
func IdentifyWithLimits(path string, limits Limits) (Format, error) {
	limits, err := limits.normalized()
	if err != nil {
		return FormatUnknown, err
	}
	f, _, err := openArchive(path, limits)
	if err != nil {
		return FormatUnknown, err
	}
	defer f.Close()
	return detectFormat(f, limits)
}

// List returns validated, normalized metadata for all members. No files are
// written and no links are followed.
func List(path string) ([]Entry, error) {
	return ListWithLimits(path, DefaultLimits())
}

// ListWithLimits returns validated metadata while enforcing streaming limits.
func ListWithLimits(path string, limits Limits) ([]Entry, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	f, _, err := openArchive(path, limits)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	format, err := detectFormat(f, limits)
	if err != nil {
		return nil, err
	}

	return listArchive(f, format, limits)
}

// Extract securely extracts path into dest. Absolute paths, parent traversal,
// links, special files, and encrypted entries are rejected before writing.
func Extract(path, dest string, progress ProgressFunc) error {
	return ExtractWithLimits(path, dest, DefaultLimits(), progress)
}

// ExtractWithLimits securely extracts an archive with caller-supplied limits.
// Existing files are replaced, but existing symlink path components are never
// followed.
func ExtractWithLimits(path, dest string, limits Limits, progress ProgressFunc) error {
	limits, err := limits.normalized()
	if err != nil {
		return err
	}
	f, _, err := openArchive(path, limits)
	if err != nil {
		return err
	}
	defer f.Close()
	format, err := detectFormat(f, limits)
	if err != nil {
		return err
	}

	entries, err := listArchive(f, format, limits)
	if err != nil {
		return err
	}
	if err := validateExtractable(entries); err != nil {
		return err
	}

	root, err := openDestination(dest)
	if err != nil {
		return err
	}
	defer root.Close()

	state := newProgressState(progress, entryTotal(entries))
	return extractFiles(root, f, format, limits, entries, state)
}

// Create writes paths to dest in format. Formats zip, tar, tar.gz, tar.xz, and
// tar.zst are supported. An empty format is inferred from dest's extension.
func Create(dest string, paths []string, format string, progress ProgressFunc) error {
	return CreateWithLimits(dest, paths, format, DefaultLimits(), progress)
}

// CreateWithLimits creates an archive atomically in dest's directory, applying
// caller-supplied limits. Source links and special files are rejected.
func CreateWithLimits(dest string, paths []string, format string, limits Limits, progress ProgressFunc) error {
	return createArchive(dest, paths, format, limits, progress)
}
