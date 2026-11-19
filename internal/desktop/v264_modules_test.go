package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestParseProcStat(t *testing.T) {
	fields := []string{
		"S", "1", "2", "3", "4", "5", "6", "7", "8", "9",
		"10", "11", "12", "13", "14", "15", "16", "17", "18", "19",
		"20", "21",
	}
	data := "1234 (my app (x)) " + strings.Join(fields, " ")
	p, ok := parseProcStat(data)
	if !ok {
		t.Fatal("parseProcStat rejected a valid record")
	}
	if p.name != "my app (x)" {
		t.Fatalf("name = %q", p.name)
	}
	if p.state != 'S' {
		t.Fatalf("state = %q", p.state)
	}
	if p.ticks != 23 || p.rssKB != 21 {
		t.Fatalf("ticks=%d rssKB=%d", p.ticks, p.rssKB)
	}
	if _, ok := parseProcStat("garbage"); ok {
		t.Fatal("parseProcStat accepted garbage")
	}
	if _, ok := parseProcStat("1 (short) S 1 2"); ok {
		t.Fatal("parseProcStat accepted a truncated record")
	}
}

func TestINIRoundTripAndRemove(t *testing.T) {
	src := "\ufeff; comment\r\n[Desktop]\r\nBackgroundColor=#102030\r\n" +
		"[Custom]\r\nName=\"hello world\"\r\nKeep=1\r\n"
	doc := parseWin95INI(src)
	if got := doc.Get("Desktop", "BackgroundColor"); got != "#102030" {
		t.Fatalf("BackgroundColor = %q", got)
	}
	if got := doc.Get("Custom", "Name"); got != "hello world" {
		t.Fatalf("quoted value = %q", got)
	}
	doc.Set("Font", "Weight", "bold")
	doc.Remove("Custom", "Keep")
	again := parseWin95INI(string(doc.Bytes()))
	if got := again.Get("Custom", "Keep"); got != "" {
		t.Fatalf("removed key survived: %q", got)
	}
	if got := again.Get("Font", "Weight"); got != "bold" {
		t.Fatalf("new key lost: %q", got)
	}
	if got := again.Get("Desktop", "BackgroundColor"); got != "#102030" {
		t.Fatalf("roundtrip lost color: %q", got)
	}
}

func TestValidateWin95Doc(t *testing.T) {
	doc := defaultWin95INI()
	if err := validateWin95Doc(doc); err != nil {
		t.Fatalf("default doc invalid: %v", err)
	}
	for _, tc := range []struct{ section, key, value string }{
		{"Desktop", "BackgroundColor", "red"},
		{"Desktop", "BackgroundStyle", "99"},
		{"Appearance", "Theme", "-1"},
		{"Locale", "Language", "xx-YY"},
		{"Font", "Family", "Comic Sans"},
	} {
		bad := defaultWin95INI()
		bad.Set(tc.section, tc.key, tc.value)
		if err := validateWin95Doc(bad); err == nil {
			t.Fatalf("%s.%s=%q was accepted", tc.section, tc.key, tc.value)
		}
	}
}

func TestScriptHelpers(t *testing.T) {
	buf := &limitedBuffer{max: 4}
	if n, err := buf.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if buf.String() != "abcd" || !buf.Truncated() {
		t.Fatalf("buffer = %q truncated=%v", buf.String(), buf.Truncated())
	}
	empty := &limitedBuffer{max: 4}
	if empty.Truncated() {
		t.Fatal("empty buffer reported truncation")
	}
	if got := shellQuote("a b'c"); got != `'a b'\''c'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestMoveToTrash(t *testing.T) {
	old := trashRoot
	trashRoot = t.TempDir()
	defer func() { trashRoot = old }()

	src := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := moveToTrash(src)
	if err != nil {
		t.Fatalf("moveToTrash: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("trashed copy = %q, %v", data, err)
	}

	second := filepath.Join(t.TempDir(), "notes")
	if err := os.WriteFile(second, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest2, err := moveToTrash(second)
	if err != nil {
		t.Fatalf("second moveToTrash: %v", err)
	}
	if dest2 == dest {
		t.Fatal("trash destination was reused")
	}
}

func TestX11ConvertPixels(t *testing.T) {
	lsb := []byte{0x33, 0x22, 0x11, 0xFF, 0x66, 0x55, 0x44, 0xFF}
	pix, hash, err := x11ConvertPixels(lsb, 2, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if pix[0] != 0x112233 || pix[1] != 0x445566 {
		t.Fatalf("LSB pixels = %#x", pix)
	}
	msb := []byte{0xFF, 0x11, 0x22, 0x33, 0xFF, 0x44, 0x55, 0x66}
	pix2, hash2, err := x11ConvertPixels(msb, 2, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if pix2[0] != pix[0] || pix2[1] != pix[1] || hash2 != hash {
		t.Fatalf("MSB conversion differs: %#x hash %d/%d", pix2, hash, hash2)
	}
	if _, _, err := x11ConvertPixels([]byte{1, 2}, 2, 1, true); err == nil {
		t.Fatal("short data was accepted")
	}
}

func TestX11Keysyms(t *testing.T) {
	if got := x11KeysymFor(KeyEvent{Name: "Enter"}); got != xproto.Keysym(0xFF0D) {
		t.Fatalf("Enter = %#x", got)
	}
	if got := x11KeysymFor(KeyEvent{Name: "F5"}); got != xproto.Keysym(0xFFC2) {
		t.Fatalf("F5 = %#x", got)
	}
	if got := x11KeysymFor(KeyEvent{Rune: 'a'}); got != xproto.Keysym('a') {
		t.Fatalf("a = %#x", got)
	}
	if got := x11KeysymFor(KeyEvent{Name: "中", Rune: '中'}); got != xproto.Keysym(0x01004E2D) {
		t.Fatalf("CJK = %#x", got)
	}
}
