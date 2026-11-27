package archive

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// normalizeName validates an archive name before it can reach the filesystem.
// It accepts both slash conventions but rejects every parent component, drive
// prefix, UNC prefix, Windows device name, and ambiguous trailing dot or space.
func normalizeName(name string, limits Limits) (normalized string, root bool, err error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return "", false, fmt.Errorf("archive: empty or NUL-containing name: %w", ErrUnsafePath)
	}
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') {
		return "", false, fmt.Errorf("archive: absolute or drive-qualified name %q: %w", name, ErrUnsafePath)
	}
	if len(name) > limits.MaxPathBytes {
		return "", false, fmt.Errorf("archive: path is too long: %w", ErrLimitExceeded)
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] == 0x7f || name[i] == ':' {
			return "", false, fmt.Errorf("archive: unsafe character in name %q: %w", name, ErrUnsafePath)
		}
	}

	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", false, fmt.Errorf("archive: parent traversal in %q: %w", name, ErrUnsafePath)
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", false, fmt.Errorf("archive: ambiguous trailing dot or space in %q: %w", name, ErrUnsafePath)
		}
		if isReservedName(part) {
			return "", false, fmt.Errorf("archive: reserved device name in %q: %w", name, ErrUnsafePath)
		}
		if len(part) > 255 {
			return "", false, fmt.Errorf("archive: path component is too long: %w", ErrLimitExceeded)
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return ".", true, nil
	}
	if len(clean) > limits.MaxDepth {
		return "", false, fmt.Errorf("archive: path is too deep: %w", ErrLimitExceeded)
	}
	return strings.Join(clean, "/"), false, nil
}

func isReservedName(name string) bool {
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

func validateEntry(entry Entry, limits Limits) (Entry, error) {
	name, root, err := normalizeName(entry.Name, limits)
	if err != nil {
		return Entry{}, err
	}
	if root && entry.Type != TypeDir {
		return Entry{}, fmt.Errorf("archive: root marker is not a directory: %w", ErrUnsafePath)
	}
	entry.Name = name
	if entry.Size < 0 {
		return Entry{}, fmt.Errorf("archive: negative member size: %w", ErrInvalidArchive)
	}
	if entry.Size > limits.MaxFileBytes {
		return Entry{}, fmt.Errorf("archive: member %q is too large: %w", name, ErrLimitExceeded)
	}
	return entry, nil
}

func validatePaths(entries []Entry, limits Limits) error {
	if len(entries) > limits.MaxEntries {
		return fmt.Errorf("archive: too many entries: %w", ErrLimitExceeded)
	}
	byName := make(map[string]Entry, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.Name == "." {
			continue
		}
		if _, exists := byName[entry.Name]; exists {
			return fmt.Errorf("archive: duplicate path %q: %w", entry.Name, ErrDuplicatePath)
		}
		byName[entry.Name] = entry
		if entry.Type == TypeFile {
			if entry.Size > limits.MaxTotalBytes-total {
				return fmt.Errorf("archive: total output size exceeds limit: %w", ErrLimitExceeded)
			}
			total += entry.Size
		}
	}
	for name, entry := range byName {
		for parent := path.Dir(name); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if p, ok := byName[parent]; ok && p.Type != TypeDir {
				return fmt.Errorf("archive: %q is both a file and a parent directory: %w", parent, ErrDuplicatePath)
			}
		}
		_ = entry
	}
	return nil
}

func validateExtractable(entries []Entry) error {
	for _, entry := range entries {
		if entry.Encrypted {
			return fmt.Errorf("archive: encrypted member %q: %w", entry.Name, ErrEncrypted)
		}
		switch entry.Type {
		case TypeFile, TypeDir:
		case TypeSymlink, TypeHardLink:
			return fmt.Errorf("archive: link member %q: %w", entry.Name, ErrLinksUnsupported)
		default:
			return fmt.Errorf("archive: special member %q: %w", entry.Name, ErrUnsupportedType)
		}
	}
	return nil
}

func entryType(mode fs.FileMode) EntryType {
	switch {
	case mode.IsDir():
		return TypeDir
	case mode&fs.ModeSymlink != 0:
		return TypeSymlink
	case mode&fs.ModeDevice != 0 || mode&fs.ModeNamedPipe != 0 || mode&fs.ModeSocket != 0 || mode&fs.ModeCharDevice != 0:
		return TypeOther
	default:
		return TypeFile
	}
}

func entryTotal(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		if entry.Type == TypeFile {
			total += entry.Size
		}
	}
	return total
}
