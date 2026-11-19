package ota

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestCloneTreeHardlinksAndSkips(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a")
	dst := filepath.Join(root, "b")
	if err := os.MkdirAll(filepath.Join(src, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "mnt", "win95-super", "slots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "etc", "hostname"), []byte("win95\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "mnt", "win95-super", "slots", "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(src, "etc", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CloneTree(src, dst); err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(filepath.Join(src, "etc", "hostname"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(dst, "etc", "hostname"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("clone did not hardlink regular files")
	}
	if _, err := os.Stat(filepath.Join(dst, "mnt", "win95-super")); !os.IsNotExist(err) {
		t.Fatalf("super mountpoint must be skipped, err=%v", err)
	}
	if got, err := os.Readlink(filepath.Join(dst, "etc", "link")); err != nil || got != "/etc/hostname" {
		t.Fatalf("symlink = %q, %v", got, err)
	}
}

func buildTestPackage(t *testing.T, dir, version string) string {
	t.Helper()
	rootfs := filepath.Join(dir, "rootfs.tar.gz")
	writeTarGz(t, rootfs, map[string]string{
		"etc/win95-version": "VERSION=" + version + "\nBUILD=test\nSLOT_LAYOUT=super-v1\n",
		"etc/new-release":   "hello " + version + "\n",
	})
	sum := fileSHA(t, rootfs)
	st, err := os.Stat(rootfs)
	if err != nil {
		t.Fatal(err)
	}
	pm := PackageManifest{
		Format: Format, Version: version, SlotLayout: Layout,
		RootfsFile: "rootfs.tar.gz", RootfsSHA256: sum, RootfsSize: st.Size(),
	}
	data, err := json.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootfsCopy := filepath.Join(pkgDir, "rootfs.tar.gz")
	data2, err := os.ReadFile(rootfs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootfsCopy, data2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "update.tar.gz")
	writeTarGz(t, pkg, map[string]string{
		"manifest.json": string(data),
		"rootfs.tar.gz": string(data2),
	})
	return pkg
}

func TestApplyUpdate(t *testing.T) {
	superDir := t.TempDir()
	slotA := SlotPath(superDir, SlotA)
	if err := os.MkdirAll(filepath.Join(slotA, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(slotA, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slotA, "etc", "win95-version"), []byte("VERSION=26.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slotA, "root", "user.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(slotA, "mnt", "win95-super"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(superDir, NewState("abcd-1234", "26.4")); err != nil {
		t.Fatal(err)
	}
	pkg := buildTestPackage(t, t.TempDir(), "26.5")

	res, err := ApplyUpdate(superDir, pkg, "abcd-1234", "26.4", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Slot != SlotB || res.PreviousSlot != SlotA || res.Version != "26.5" {
		t.Fatalf("result = %+v", res)
	}
	slotB := SlotPath(superDir, SlotB)
	if data, err := os.ReadFile(filepath.Join(slotB, "etc", "new-release")); err != nil || string(data) != "hello 26.5\n" {
		t.Fatalf("new release file = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(slotB, "root", "user.txt")); err != nil || string(data) != "keep me" {
		t.Fatalf("user data not preserved = %q, %v", data, err)
	}
	fstab, err := os.ReadFile(filepath.Join(slotB, "etc", "fstab"))
	if err != nil || !strings.Contains(string(fstab), "PARTUUID=abcd-1234 /mnt/win95-super") {
		t.Fatalf("fstab = %q, %v", fstab, err)
	}
	st, err := ReadState(superDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != SlotA || st.Trial != SlotB || st.Slots[SlotB].Version != "26.5" {
		t.Fatalf("state = %+v", st)
	}
	if data, err := os.ReadFile(filepath.Join(slotA, "etc", "win95-version")); err != nil || !strings.Contains(string(data), "26.4") {
		t.Fatalf("active slot changed: %q, %v", data, err)
	}
}

func TestFetchManifestAndDownload(t *testing.T) {
	payload := []byte(strings.Repeat("ota-payload-", 10000))
	sum := sha256.Sum256(payload)
	manifest := Manifest{
		Format: Format, Version: "26.5", SlotLayout: Layout,
		Package: PackageRef{
			URL:    "https://example.invalid/update.tar.gz",
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload)),
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	})
	mux.HandleFunc("/update.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "update.tar.gz", timeZero, strings.NewReader(string(payload)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := FetchManifest(context.Background(), srv.URL+"/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "26.5" || got.Package.Size != int64(len(payload)) {
		t.Fatalf("manifest = %+v", got)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar.gz")
	part := dest + ".part"
	if err := os.WriteFile(part, payload[:12345], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DownloadFile(context.Background(), srv.URL+"/update.tar.gz", dest, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(data), len(payload))
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("partial file was not renamed")
	}
	if got, _ := SHA256File(dest); got != manifest.Package.SHA256 {
		t.Fatalf("sha = %s", got)
	}
}

func TestExtractPackageRejectsBadHash(t *testing.T) {
	dir := t.TempDir()
	pkg := buildTestPackage(t, dir, "26.5")
	scratch := filepath.Join(dir, "scratch")
	if _, _, err := ExtractPackage(pkg, scratch, nil); err != nil {
		t.Fatal(err)
	}
	// Corrupt the rootfs inside the package by rebuilding it with a bad sum.
	pmPath := filepath.Join(scratch, "manifest.json")
	pm, err := ReadPackageManifest(scratch)
	if err != nil {
		t.Fatal(err)
	}
	pm.RootfsSHA256 = strings.Repeat("0", 64)
	data, err := json.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pmPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	badPkg := filepath.Join(dir, "bad.tar.gz")
	writeTarGz(t, badPkg, map[string]string{
		"manifest.json": string(data),
		"rootfs.tar.gz": string(mustRead(t, filepath.Join(scratch, "rootfs.tar.gz"))),
	})
	if _, _, err := ExtractPackage(badPkg, filepath.Join(dir, "scratch2"), nil); err == nil {
		t.Fatal("corrupt package must be rejected")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var timeZero = time.Time{}
