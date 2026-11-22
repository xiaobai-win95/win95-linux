package lanshare

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadDownloadAndToken(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Start(); err != nil {
		t.Skipf("cannot bind LAN share port in this environment: %v", err)
	}
	defer s.Stop()
	base := "http://127.0.0.1:" + itoa(s.Port())

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte("局域网快传")) {
		t.Fatalf("index: status=%d body=%.120s", resp.StatusCode, body)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "测试 文件.txt")
	_, _ = fw.Write([]byte("hello lan share"))
	mw.Close()
	req, _ := http.NewRequest("POST", base+"/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte("测试 文件.txt")) {
		t.Fatalf("upload: status=%d body=%s", resp.StatusCode, body)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "测试 文件.txt"))
	if err != nil || string(saved) != "hello lan share" {
		t.Fatalf("saved file: %v %q", err, saved)
	}

	resp, err = http.Get(base + "/d/" + "测试%20文件.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "hello lan share" {
		t.Fatalf("download: status=%d body=%q", resp.StatusCode, body)
	}

	outside := filepath.Join(t.TempDir(), "outside.bin")
	_ = os.WriteFile(outside, []byte{1, 2, 3, 4}, 0o644)
	link, err := s.RegisterFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]
	resp, err = http.Get(base + "/s/" + token)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("token download: status=%d body=%v", resp.StatusCode, got)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		`..\..\win.ini`:    "win.ini",
		"":                 "未命名文件",
		"..":               "未命名文件",
		"a\x00b.txt":       "ab.txt",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	got := s.uniquePath("a.txt")
	if got != filepath.Join(dir, "a (2).txt") {
		t.Fatalf("uniquePath = %q", got)
	}
}

func TestStatsAfterUpload(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Start(); err != nil {
		t.Skipf("cannot bind: %v", err)
	}
	defer s.Stop()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "x.bin")
	_, _ = fw.Write([]byte("abc"))
	mw.Close()
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+itoa(s.Port())+"/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	uploads, last, when, upErr := s.Stats()
	if uploads != 1 || last != "x.bin" || upErr != "" || time.Since(when) > time.Minute {
		t.Fatalf("stats = %d %q %v %q", uploads, last, when, upErr)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
