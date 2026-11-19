package ota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSourcesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ota.conf")
	conf := "# comment\n" +
		"MANIFEST_URL=https://example.com/win95/latest.json\n" +
		"MIRROR=https://mirror.example/\n" +
		"MIRROR=https://tpl.example/fetch?url={url}\n" +
		"DIRECT=0\n"
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	s := loadSources(path)
	if s.ManifestURL != "https://example.com/win95/latest.json" {
		t.Fatalf("manifest = %q", s.ManifestURL)
	}
	if len(s.Mirrors) != 2 || s.Mirrors[0] != "https://mirror.example/" {
		t.Fatalf("mirrors = %v", s.Mirrors)
	}
	if s.Direct {
		t.Fatal("DIRECT=0 must disable the direct GitHub source")
	}

	missing := loadSources(filepath.Join(t.TempDir(), "absent.conf"))
	if missing.ManifestURL != DefaultManifestURL || !missing.Direct || len(missing.Mirrors) == 0 {
		t.Fatalf("missing config must keep defaults: %+v", missing)
	}
}

func TestSourceCandidates(t *testing.T) {
	github := "https://github.com/xiaobai-win95/win95-linux/releases/download/v26.4/pkg.tar.gz"
	s := Sources{ManifestURL: "https://github.com/o/r/releases/latest/download/latest.json", Mirrors: []string{"https://m.example/", "https://tpl.example/?u={url}"}, Direct: true}

	got := s.PackageCandidates(github)
	want := []string{
		github,
		"https://m.example/https://github.com/xiaobai-win95/win95-linux/releases/download/v26.4/pkg.tar.gz",
		"https://tpl.example/?u=" + github,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if m := s.ManifestCandidates(); len(m) != 3 || m[0] != s.ManifestURL {
		t.Fatalf("manifest candidates = %v", m)
	}

	custom := "https://example.com/pkg.tar.gz"
	if got := s.PackageCandidates(custom); len(got) != 1 || got[0] != custom {
		t.Fatalf("non-GitHub URL must not be mirrored: %v", got)
	}

	s.Direct = false
	got = s.PackageCandidates(github)
	if len(got) != 2 || got[0] == github {
		t.Fatalf("DIRECT=0 must skip the direct URL: %v", got)
	}

	if SourceLabel(github) != "GitHub 直连" || !strings.Contains(SourceLabel("https://ghfast.top/x"), "ghfast.top") {
		t.Fatal("unexpected source labels")
	}
}

func TestFetchManifestAutoRace(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"format":1,"version":"26.4","slot_layout":"super-v1","package":{"url":"https://example.com/pkg.tar.gz","sha256":"` + strings.Repeat("a", 64) + `","size":123}}`))
	}))
	defer good.Close()

	var logs []string
	m, used, err := FetchManifestAuto(context.Background(), []string{bad.URL + "/latest.json", good.URL + "/latest.json"}, func(s string) {
		logs = append(logs, s)
	})
	if err != nil {
		t.Fatalf("FetchManifestAuto: %v", err)
	}
	if m.Version != "26.4" || used != good.URL+"/latest.json" {
		t.Fatalf("manifest=%+v used=%s", m, used)
	}
	if len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "更新清单来源") {
		t.Fatalf("expected a source log line, got %v", logs)
	}

	if _, _, err := FetchManifestAuto(context.Background(), []string{bad.URL + "/latest.json"}, nil); err == nil {
		t.Fatal("all-failing sources must return an error")
	}
}

func TestDownloadPackageAutoFallback(t *testing.T) {
	payload := "hello-ota-package"
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-0/1")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
			return
		}
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer broken.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer good.Close()

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	var logs []string
	err := DownloadPackageAuto(context.Background(), []string{broken.URL + "/pkg", good.URL + "/pkg"}, dest,
		func(done, total int64) {}, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatalf("DownloadPackageAuto: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Fatalf("download = %q", data)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("partial file must be renamed on success")
	}
	if !strings.Contains(strings.Join(logs, "\n"), "下载来源") {
		t.Fatalf("expected a download source log line, got %v", logs)
	}
}
