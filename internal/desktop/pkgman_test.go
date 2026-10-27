package desktop

import (
	"strings"
	"testing"
)

func TestSplitNameVersion(t *testing.T) {
	cases := []struct{ in, name, ver string }{
		{"apk-tools-3.0.8-r0", "apk-tools", "3.0.8-r0"},
		{"py3-requests-2.31.0-r1", "py3-requests", "2.31.0-r1"},
		{"musl", "musl", ""},
		{"gtk+2.0-dev", "gtk+2.0-dev", ""},
		{"font-noto-cjk-20230817-r0", "font-noto-cjk", "20230817-r0"},
	}
	for _, c := range cases {
		name, ver := splitNameVersion(c.in)
		if name != c.name || ver != c.ver {
			t.Errorf("splitNameVersion(%q) = %q, %q; want %q, %q", c.in, name, ver, c.name, c.ver)
		}
	}
}

func TestParseSearchOutput(t *testing.T) {
	out := strings.Join([]string{
		"apk-tools-3.0.8-r0 - Alpine Package Keeper",
		"apk-tools-doc-3.0.8-r0 - Alpine Package Keeper (documentation)",
		"",
		"busybox-1.37.0-r12 - Size optimized toolbox of many common UNIX utilities",
		"apk-tools-3.0.9-r0 - duplicate should be ignored",
	}, "\n")
	list := parseSearchOutput(out)
	if len(list) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(list), list)
	}
	if list[0].Name != "apk-tools" || list[0].Version != "3.0.8-r0" || list[0].Desc != "Alpine Package Keeper" {
		t.Errorf("unexpected first entry: %+v", list[0])
	}
	if list[1].Name != "apk-tools-doc" || list[1].Version != "3.0.8-r0" {
		t.Errorf("unexpected second entry: %+v", list[1])
	}
	if list[2].Name != "busybox" || list[2].Version != "1.37.0-r12" {
		t.Errorf("unexpected third entry: %+v", list[2])
	}
}

func TestParseSearchOutputWithoutDescription(t *testing.T) {
	list := parseSearchOutput("busybox-1.37.0-r12\n")
	if len(list) != 1 || list[0].Name != "busybox" || list[0].Desc != "" {
		t.Fatalf("unexpected result: %+v", list)
	}
}

func TestParseInstalledDB(t *testing.T) {
	db := strings.Join([]string{
		"C:Q1abc=",
		"P:musl",
		"V:1.2.5-r0",
		"A:aarch64",
		"T:the musl c library (libc) implementation",
		"",
		"P:busybox",
		"V:1.37.0-r12",
		"T:Size optimized toolbox",
		"",
		"",
	}, "\n")
	got := parseInstalledDB(strings.NewReader(""))
	if len(got) != 0 {
		t.Fatalf("empty input produced %d entries", len(got))
	}
	m := parseInstalledDB(strings.NewReader(db))
	if len(m) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(m), m)
	}
	if e := m["musl"]; e.Version != "1.2.5-r0" || !strings.Contains(e.Desc, "musl") {
		t.Errorf("unexpected musl entry: %+v", e)
	}
	if e := m["busybox"]; e.Version != "1.37.0-r12" || e.Desc != "Size optimized toolbox" {
		t.Errorf("unexpected busybox entry: %+v", e)
	}
}

func TestErrText(t *testing.T) {
	if got := errText("", nil); got != "未知错误" {
		t.Errorf("errText(empty) = %q", got)
	}
	if got := errText("one\ntwo\nlast line\n", nil); got != "last line" {
		t.Errorf("errText(stderr) = %q", got)
	}
}
