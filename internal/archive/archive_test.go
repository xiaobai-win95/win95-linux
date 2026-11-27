package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testArchiveEntry struct {
	name     string
	data     []byte
	mode     fs.FileMode
	typeflag byte
	linkname string
}

func writeTestZIP(t *testing.T, path string, entries []testArchiveEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		if strings.HasSuffix(entry.name, "/") {
			mode |= fs.ModeDir
		}
		h.SetMode(mode)
		h.SetModTime(time.Unix(1_700_000_000, 0))
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestTar(t *testing.T, path string, entries []testArchiveEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			if strings.HasSuffix(entry.name, "/") {
				typeflag = tar.TypeDir
			} else {
				typeflag = tar.TypeReg
			}
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{
			Name:     entry.name,
			Mode:     int64(mode.Perm()),
			Size:     int64(len(entry.data)),
			ModTime:  time.Unix(1_700_000_000, 0),
			Typeflag: typeflag,
			Linkname: entry.linkname,
			Format:   tar.FormatPAX,
		}
		if typeflag != tar.TypeReg && typeflag != tar.TypeRegA {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tw.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCreateListExtractRoundTrip(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "sub", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"hello.txt":       []byte("hello, archive\n"),
		"sub/data.bin":    {0, 1, 2, 3, 0xff},
		"sub/empty/file":  {},
		"sub/empty/other": []byte("other"),
	}
	for name, data := range files {
		full := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	formats := []struct {
		format   string
		ext      string
		detected Format
	}{
		{"zip", ".zip", FormatZIP},
		{"tar", ".tar", FormatTar},
		{"tar.gz", ".tar.gz", FormatTarGZ},
		{"tar.xz", ".tar.xz", FormatTarXZ},
		{"tar.zst", ".tar.zst", FormatTarZST},
	}
	for _, tc := range formats {
		t.Run(tc.format, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "bundle"+tc.ext)
			var callbacks int
			var finalDone, finalTotal int64
			err := Create(archivePath, []string{source}, tc.format, func(done, total int64, current string) {
				callbacks++
				if done < 0 || done > total {
					t.Errorf("invalid progress: %d/%d", done, total)
				}
				finalDone, finalTotal = done, total
			})
			if err != nil {
				t.Fatal(err)
			}
			if callbacks == 0 || finalDone != finalTotal || finalTotal == 0 {
				t.Fatalf("progress callbacks=%d final=%d/%d", callbacks, finalDone, finalTotal)
			}
			format, err := Identify(archivePath)
			if err != nil || format != tc.detected {
				t.Fatalf("Identify() = %q, %v; want %q", format, err, tc.detected)
			}
			entries, err := List(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(source)
			for name := range files {
				want := pathForArchive(base + "/" + name)
				if !hasEntry(entries, want, TypeFile) {
					t.Errorf("List() missing %q; entries=%v", want, entries)
				}
			}
			if !hasEntry(entries, pathForArchive(base+"/sub/empty"), TypeDir) {
				t.Errorf("List() missing empty directory; entries=%v", entries)
			}
			dest := t.TempDir()
			if err := Extract(archivePath, dest, nil); err != nil {
				t.Fatal(err)
			}
			for name, want := range files {
				got := readTestFile(t, filepath.Join(dest, filepath.FromSlash(base+"/"+name)))
				if !bytes.Equal(got, want) {
					t.Errorf("extracted %q = %q; want %q", name, got, want)
				}
			}
			if info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(base+"/sub/empty"))); err != nil || !info.IsDir() {
				t.Fatalf("empty directory was not extracted: %v", err)
			}
		})
	}
}

func TestCreateEmptyArchives(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz", "tar.xz", "tar.zst"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "empty."+format)
			if err := Create(path, nil, format, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := Identify(path); err != nil {
				t.Fatal(err)
			}
			entries, err := List(path)
			if err != nil || len(entries) != 0 {
				t.Fatalf("List() = %v, %v; want empty", entries, err)
			}
			if err := Extract(path, t.TempDir(), nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIdentifyUsesContents(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "actually-gzip.zip")
	writeTestZIP(t, zipPath, []testArchiveEntry{{name: "file", data: []byte("zip")}})
	if got, err := Identify(zipPath); err != nil || got != FormatZIP {
		t.Fatalf("Identify(zip with wrong extension) = %q, %v", got, err)
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Identify(plain); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Identify(plain) error = %v; want ErrUnsupportedFormat", err)
	}

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write([]byte("not a tar stream")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	gzipPath := filepath.Join(dir, "data.tar.gz")
	if err := os.WriteFile(gzipPath, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Identify(gzipPath); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Identify(gzip non-tar) error = %v; want ErrUnsupportedFormat", err)
	}
}

func TestCreateFormatAliasesAndInference(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		format string
		want   Format
	}{
		{"inferred.tgz", "", FormatTarGZ},
		{"inferred.txz", "", FormatTarXZ},
		{"inferred.tzst", "", FormatTarZST},
		{"alias.bin", "TGZ", FormatTarGZ},
		{"alias.bin", "zstd", FormatTarZST},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := Create(path, []string{source}, tc.format, nil); err != nil {
				t.Fatal(err)
			}
			if got, err := Identify(path); err != nil || got != tc.want {
				t.Fatalf("Identify() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if err := Create(filepath.Join(t.TempDir(), "bad"), nil, "rar", nil); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Create(rar) error = %v; want ErrUnsupportedFormat", err)
	}
}

func TestCreateRejectsLinksAndRecursiveDestination(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if err := Create(filepath.Join(t.TempDir(), "archive.zip"), []string{source}, "zip", nil); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Create(with symlink) error = %v; want ErrUnsupportedType", err)
	}
	if err := os.Remove(filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	insideOutput := filepath.Join(source, "archive.zip")
	if err := Create(insideOutput, []string{source}, "zip", nil); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Create(inside source) error = %v; want ErrUnsafePath", err)
	}
	if _, err := os.Stat(insideOutput); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("recursive output was created: %v", err)
	}
}

func hasEntry(entries []Entry, name string, typ EntryType) bool {
	for _, entry := range entries {
		if entry.Name == name && entry.Type == typ {
			return true
		}
	}
	return false
}

func pathForArchive(name string) string {
	return strings.ReplaceAll(name, `\`, "/")
}

func copyTestBytes(dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)
	return err
}

func TestListReturnsNormalizedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.zip")
	writeTestZIP(t, path, []testArchiveEntry{{name: `dir\file.txt`, data: []byte("data"), mode: 0o640}})
	entries, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{
		Name:    "dir/file.txt",
		Size:    4,
		ModTime: time.Unix(1_700_000_000, 0),
		Mode:    0o640,
		Type:    TypeFile,
	}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("List() = %#v; want %#v", entries, want)
	}
}

func ExampleCreate() {
	dir, err := os.MkdirTemp("", "archive-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		panic(err)
	}
	if err := Create(filepath.Join(dir, "hello.zip"), []string{source}, "zip", nil); err != nil {
		panic(err)
	}
	format, err := Identify(filepath.Join(dir, "hello.zip"))
	fmt.Println(format, err)
	// Output: zip <nil>
}
