package desktop

import (
	"os"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func fakeOfficeInstalled(componentIDs ...string) map[string]bool {
	pkgs := make(map[string]bool)
	for _, id := range componentIDs {
		c := officeCompByID(id)
		if c == nil {
			continue
		}
		for _, p := range c.Packages {
			pkgs[p] = true
		}
	}
	return pkgs
}

func TestParseAPKInstalled(t *testing.T) {
	data := "C:Q1abc=\nP:libreoffice-common\nV:25.8.7.3-r0\n\nC:Q1def=\nP:xvfb\nV:21.1.26-r0\n"
	pkgs := parseAPKInstalled(data)
	if !pkgs["libreoffice-common"] || !pkgs["xvfb"] || len(pkgs) != 2 {
		t.Fatalf("parseAPKInstalled = %v", pkgs)
	}
}

func TestOfficeFileModuleRouting(t *testing.T) {
	cases := map[string]string{
		"报告.docx":     "writer",
		"报表.XLSX":     "calc",
		"幻灯片.pptx":    "impress",
		"流程图.odg":     "draw",
		"数据.odb":      "base",
		"手册.pdf":      "pdf",
		"notes.txt":   "",
		"photo.png":   "",
		"archive.zip": "",
	}
	for name, want := range cases {
		if got := officeFileModule(name); got != want {
			t.Errorf("officeFileModule(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOfficeSelectionAndMissingPackages(t *testing.T) {
	installed := fakeOfficeInstalled("core")
	sel := officeSelectionDefaults(installed)
	if !sel["core"] || !sel["writer"] || !sel["calc"] || !sel["impress"] || !sel["pdf"] || !sel["ime"] {
		t.Fatalf("defaults should include core and recommended modules: %v", sel)
	}
	if sel["base"] || sel["mail"] || sel["ocr"] || sel["cloud"] {
		t.Fatalf("optional components must be off by default: %v", sel)
	}
	if !officeCompInstalled(*officeCompByID("core"), installed) {
		t.Fatal("core should be detected as installed")
	}

	pkgs := officeMissingPackages(sel, installed)
	if len(pkgs) == 0 {
		t.Fatal("expected missing packages for uninstalled modules")
	}
	seen := map[string]bool{}
	for _, p := range pkgs {
		if seen[p] {
			t.Fatalf("duplicate package %q in missing list", p)
		}
		seen[p] = true
		if installed[p] {
			t.Fatalf("package %q is installed but listed as missing", p)
		}
	}
	if !seen["libreoffice-writer"] || !seen["libreoffice-calc"] || !seen["fcitx5"] {
		t.Fatalf("missing list lacks expected packages: %v", pkgs)
	}

	empty := officeMissingPackages(map[string]bool{"core": true}, map[string]bool{})
	if len(empty) != len(officeCompByID("core").Packages) {
		t.Fatalf("empty install should request every core package, got %v", empty)
	}
}

func TestOfficeAddonSelectionKeepsInstalled(t *testing.T) {
	installed := fakeOfficeInstalled("core", "writer", "calc", "impress")
	sel := officeSelectionDefaults(installed)
	if !sel["writer"] || !sel["calc"] || !sel["impress"] {
		t.Fatalf("installed modules must stay selected: %v", sel)
	}
	if officeCompInstalled(*officeCompByID("base"), installed) {
		t.Fatal("base should not be installed")
	}
	sel["base"] = true
	pkgs := officeMissingPackages(sel, installed)
	found := false
	for _, p := range pkgs {
		if p == "openjdk17-jre-headless" {
			found = true
		}
	}
	if !found {
		t.Fatalf("base add-on should pull Java, got %v", pkgs)
	}
	if n := officeMissingCount(sel, installed); n < 3 {
		t.Fatalf("missing component count = %d, want >= 3", n)
	}
}

func TestPreviewOffice(t *testing.T) {
	_ = os.MkdirAll("/Users/madic/aiwork/win95linux/out", 0o755)
	d := New(1024, 768, ModeDesktop, Host{})
	canvas := ui.NewCanvas(1024, 768)
	d.clock = "09:41"

	suite := NewOfficeApp(d)
	suite.installed = fakeOfficeInstalled("core", "writer", "calc", "impress", "draw", "math", "pdf", "ime", "langs")
	w := d.Open(suite)
	suite.win = w
	suite.lastRefresh = time.Now()
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-office.png")
	d.CloseWindow(w)

	setup := NewOfficeSetupApp(d, "base", "", "")
	setup.installed = fakeOfficeInstalled("core", "writer", "calc", "impress", "draw", "math", "pdf", "ime", "langs")
	setup.sel = officeSelectionDefaults(setup.installed)
	setup.sel["base"] = true
	setup.msg = "首次安装可以只选择需要的组件；安装完成后随时回到这里补装。"
	sw := d.Open(setup)
	setup.win = sw
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-office-setup.png")
}
