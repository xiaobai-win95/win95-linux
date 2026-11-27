package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectUnsafePathsBeforeWriting(t *testing.T) {
	unsafe := []string{
		"../escape.txt",
		"safe/../../escape.txt",
		"/absolute.txt",
		`C:\escape.txt`,
		`\\server\share\escape.txt`,
		"..",
	}
	for _, name := range unsafe {
		for _, kind := range []string{"zip", "tar.gz"} {
			t.Run(kind+"/"+strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "malicious."+kind)
				entries := []testArchiveEntry{
					{name: "safe.txt", data: []byte("must not be written")},
					{name: name, data: []byte("attack")},
				}
				if kind == "zip" {
					writeTestZIP(t, path, entries)
				} else {
					writeTestTar(t, path, entries)
					compressTestGZIP(t, path)
				}
				if _, err := List(path); !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("List() error = %v; want ErrUnsafePath", err)
				}
				dest := t.TempDir()
				if err := Extract(path, dest, nil); !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("Extract() error = %v; want ErrUnsafePath", err)
				}
				if _, err := os.Stat(filepath.Join(dest, "safe.txt")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("safe file was written before malicious entry was rejected: %v", err)
				}
			})
		}
	}
}

func TestRejectLinks(t *testing.T) {
	for _, kind := range []string{"zip-symlink", "tar-symlink", "tar-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "link."+strings.Split(kind, "-")[0])
			if strings.HasPrefix(kind, "zip") {
				writeTestZIP(t, path, []testArchiveEntry{{name: "link", data: []byte("../../outside"), mode: fs.ModeSymlink | 0o777}})
			} else {
				typeflag := byte(tar.TypeSymlink)
				if kind == "tar-hardlink" {
					typeflag = tar.TypeLink
				}
				writeTestTar(t, path, []testArchiveEntry{{name: "link", typeflag: typeflag, linkname: "../../outside", mode: 0o777}})
			}
			entries, err := List(path)
			if err != nil {
				t.Fatal(err)
			}
			wantType := TypeSymlink
			if kind == "tar-hardlink" {
				wantType = TypeHardLink
			}
			if len(entries) != 1 || entries[0].Type != wantType || entries[0].LinkTarget == "" {
				t.Fatalf("List() = %#v; want link metadata", entries)
			}
			dest := t.TempDir()
			if err := Extract(path, dest, nil); !errors.Is(err, ErrLinksUnsupported) {
				t.Fatalf("Extract() error = %v; want ErrLinksUnsupported", err)
			}
			if _, err := os.Lstat(filepath.Join(dest, "link")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("link was materialized: %v", err)
			}
		})
	}
}

func TestRejectPreexistingSymlinkParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	writeTestZIP(t, path, []testArchiveEntry{{name: "link/evil.txt", data: []byte("evil")}})
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Fatal(err)
	}
	if err := Extract(path, dest, nil); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Extract() error = %v; want ErrUnsafePath", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("archive escaped through destination symlink: %v", err)
	}
}

func TestReplaceFinalSymlinkWithoutFollowingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	writeTestZIP(t, path, []testArchiveEntry{{name: "file", data: []byte("new")}})
	dest := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "file")); err != nil {
		t.Fatal(err)
	}
	if err := Extract(path, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(readTestFile(t, outside)); got != "old" {
		t.Fatalf("outside file changed to %q", got)
	}
	if got := string(readTestFile(t, filepath.Join(dest, "file"))); got != "new" {
		t.Fatalf("extracted file = %q; want new", got)
	}
	info, err := os.Lstat(filepath.Join(dest, "file"))
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		t.Fatalf("destination is still a symlink: %v", err)
	}
}

func TestRejectDuplicateAndConflictingPaths(t *testing.T) {
	cases := []struct {
		name    string
		entries []testArchiveEntry
	}{
		{"duplicate", []testArchiveEntry{{name: "same", data: []byte("a")}, {name: "same", data: []byte("b")}}},
		{"normalized-duplicate", []testArchiveEntry{{name: `dir\same`, data: []byte("a")}, {name: "dir/same", data: []byte("b")}}},
		{"file-parent", []testArchiveEntry{{name: "a/b", data: []byte("child")}, {name: "a", data: []byte("parent")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conflict.zip")
			writeTestZIP(t, path, tc.entries)
			if _, err := List(path); !errors.Is(err, ErrDuplicatePath) {
				t.Fatalf("List() error = %v; want ErrDuplicatePath", err)
			}
			if err := Extract(path, t.TempDir(), nil); !errors.Is(err, ErrDuplicatePath) {
				t.Fatalf("Extract() error = %v; want ErrDuplicatePath", err)
			}
		})
	}
}

func TestStreamingLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limits.zip")
	data := bytes.Repeat([]byte("x"), 128)
	writeTestZIP(t, path, []testArchiveEntry{{name: "one", data: data}, {name: "two", data: data}})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		limits Limits
	}{
		{"archive", Limits{MaxArchiveBytes: info.Size() - 1}},
		{"file", Limits{MaxFileBytes: int64(len(data) - 1)}},
		{"total", Limits{MaxTotalBytes: int64(len(data) + 1)}},
		{"entries", Limits{MaxEntries: 1}},
		{"path", Limits{MaxPathBytes: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "archive" {
				if _, err := IdentifyWithLimits(path, tc.limits); !errors.Is(err, ErrLimitExceeded) {
					t.Fatalf("IdentifyWithLimits() error = %v; want ErrLimitExceeded", err)
				}
			}
			if _, err := ListWithLimits(path, tc.limits); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
			}
			if err := ExtractWithLimits(path, t.TempDir(), tc.limits, nil); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("ExtractWithLimits() error = %v; want ErrLimitExceeded", err)
			}
		})
	}
}

func TestPathDepthAndWindowsNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paths.zip")
	writeTestZIP(t, path, []testArchiveEntry{{name: "a/b/file", data: []byte("data")}})
	if _, err := ListWithLimits(path, Limits{MaxDepth: 2}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth limit error = %v; want ErrLimitExceeded", err)
	}
	for _, name := range []string{"CON", "nul.txt", "LPT1", "trailing.", "trailing "} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "windows-name.zip")
			writeTestZIP(t, path, []testArchiveEntry{{name: name, data: []byte("data")}})
			if _, err := List(path); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("List(%q) error = %v; want ErrUnsafePath", name, err)
			}
		})
	}
}

func TestCompressedTarTrailingBombIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tar.gz")
	writeTestTar(t, path, []testArchiveEntry{{name: "small", data: []byte("ok")}})
	compressTestGZIP(t, path)
	original := readTestFile(t, path)
	var trailing bytes.Buffer
	gz := gzip.NewWriter(&trailing)
	if _, err := gz.Write(bytes.Repeat([]byte{0}, 8192)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(original, trailing.Bytes()...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListWithLimits(path, Limits{MaxTotalBytes: 4096}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
	}
}

func TestCreateLimits(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "large"), bytes.Repeat([]byte("x"), 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateWithLimits(filepath.Join(t.TempDir(), "archive.zip"), []string{source}, "zip", Limits{MaxFileBytes: 100}, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("CreateWithLimits() error = %v; want ErrLimitExceeded", err)
	}
}

func compressTestGZIP(t *testing.T, path string) {
	t.Helper()
	data := readTestFile(t, path)
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
