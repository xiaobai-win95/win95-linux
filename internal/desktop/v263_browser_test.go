package desktop

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func testPNGDataURI(t *testing.T) string {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for x := 0; x < 6; x++ {
		for y := 0; y < 4; y++ {
			m.Set(x, y, color.RGBA{R: uint8(x * 40), G: uint8(y * 50), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func controlsByID(doc *bDoc) map[string]*bControl {
	m := make(map[string]*bControl)
	for _, line := range doc.lines {
		for _, sp := range line.spans {
			if sp.ctrl == nil {
				continue
			}
			if id := attr(sp.ctrl.node, "id"); id != "" {
				m[id] = sp.ctrl
			}
		}
	}
	return m
}

func TestBrowserImagesControlsAndCSS(t *testing.T) {
	dataURI := testPNGDataURI(t)
	raw := `<!DOCTYPE html><html><head><title>控件</title>
<style>
.demo-btn { background-color: #000080; color: #ffffff; font-weight: bold; }
</style></head><body>
<img id="pic" src="` + dataURI + `" width="3">
<button id="ok" class="demo-btn">确定</button>
<button id="plain">普通</button>
<input id="name" value="Windows">
<textarea id="notes" rows="2" cols="12">你好</textarea>
<select id="pick"><option>甲</option><option selected>乙</option></select>
<input type="checkbox" id="agree" checked>
</body></html>`

	root, err := parseHTMLRoot(raw)
	if err != nil {
		t.Fatal(err)
	}
	img, err := fetchImageResource(dataURI)
	if err != nil {
		t.Fatalf("data URI image failed to decode: %v", err)
	}
	if img.w != 6 || img.h != 4 {
		t.Fatalf("decoded image is %dx%d, want 6x4", img.w, img.h)
	}
	doc := layoutDOMImages(root, 600, func(n *xhtml.Node) *bImage {
		if attr(n, "id") == "pic" {
			return img
		}
		return nil
	})
	ctrls := controlsByID(doc)
	for _, id := range []string{"pic", "ok", "plain", "name", "notes", "pick", "agree"} {
		if ctrls[id] == nil {
			t.Fatalf("control %q was not laid out", id)
		}
	}

	pic := ctrls["pic"]
	if pic.tag != "img" || pic.img == nil || pic.w != 3 || pic.h != 2 {
		t.Fatalf("image control = tag %q %dx%d, want scaled 3x2", pic.tag, pic.w, pic.h)
	}

	ok := ctrls["ok"]
	if ok.tag != "button" || ok.label != "确定" {
		t.Fatalf("button = %q %q", ok.tag, ok.label)
	}
	if !ok.hasBg || ok.colBg != 0x000080 {
		t.Fatalf("stylesheet background not applied: hasBg=%v colBg=%06X", ok.hasBg, ok.colBg)
	}
	if !ok.hasFg || ok.colFg != 0xffffff {
		t.Fatalf("stylesheet color not applied: hasFg=%v colFg=%06X", ok.hasFg, ok.colFg)
	}
	if plain := ctrls["plain"]; plain.hasBg {
		t.Fatal("button without a class should not gain a background")
	}

	if name := ctrls["name"]; name.tag != "input" || name.value != "Windows" {
		t.Fatalf("input = %q %q", name.tag, name.value)
	}
	if notes := ctrls["notes"]; notes.tag != "textarea" || notes.value != "你好" {
		t.Fatalf("textarea = %q %q", notes.tag, notes.value)
	}
	if pick := ctrls["pick"]; pick.tag != "select" || pick.label != "乙" {
		t.Fatalf("select = %q label %q", pick.tag, pick.label)
	}
	if agree := ctrls["agree"]; agree.tag != "checkbox" || !agree.checked {
		t.Fatal("checkbox checked state was not read")
	}
}

func TestBrowserTypingFiresInputEvent(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:forms"}
	a.raw = `<html><body>
<input id="t" value="" oninput="mirror()">
<p id="out"></p>
<script>
function mirror() {
  document.getElementById("out").textContent =
    "值:" + document.getElementById("t").value;
}
</script>
</body></html>`
	a.kind = pageHTML
	a.startPageDOM()
	if a.js == nil {
		t.Fatal("JS runtime missing")
	}
	inp := findID(a.dom, "t")
	a.focusControl(inp)
	if !a.keyToControl(KeyEvent{Rune: 'A'}, d) {
		t.Fatal("typing was not consumed by the focused input")
	}
	a.keyToControl(KeyEvent{Rune: 'B'}, d)
	if got := attr(inp, "value"); got != "AB" {
		t.Fatalf("input value = %q, want AB", got)
	}
	if got := textContentOf(findID(a.dom, "out")); got != "值:AB" {
		t.Fatalf("oninput did not run: %q", got)
	}
	if !a.keyToControl(KeyEvent{Name: "Backspace"}, d) {
		t.Fatal("backspace was not handled")
	}
	if got := attr(inp, "value"); got != "A" {
		t.Fatalf("after backspace value = %q, want A", got)
	}
	a.keyToControl(KeyEvent{Rune: 'x', Ctrl: true}, d)
}

func TestBrowserSelectCycleAndCheckbox(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:select"}
	a.raw = `<html><body>
<select id="s"><option value="a">甲</option><option value="b">乙</option></select>
<input type="checkbox" id="c">
<script>
var changes = 0;
document.getElementById("s").onchange = function () { changes++; };
</script>
</body></html>`
	a.kind = pageHTML
	a.startPageDOM()
	doc := layoutDOMImages(a.dom, 600, nil)
	ctrls := controlsByID(doc)
	sel := ctrls["s"]
	if sel == nil {
		t.Fatal("select control missing")
	}
	a.cycleSelect(sel, d)
	if sel.label != "乙" || sel.value != "b" || attr(sel.node, "value") != "b" {
		t.Fatalf("select did not cycle: label %q value %q", sel.label, sel.value)
	}
	box := ctrls["c"]
	if box == nil {
		t.Fatal("checkbox control missing")
	}
	a.controlDown(&linkHit{ctrl: box}, MouseEvent{}, d)
	if !box.checked {
		t.Fatal("checkbox did not toggle on")
	}
	a.controlDown(&linkHit{ctrl: box}, MouseEvent{}, d)
	if box.checked {
		t.Fatal("checkbox did not toggle off")
	}
}

func TestBrowserMCJSGateway(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1}
	a.Navigate("mcjs.link")
	if a.curURL != "https://mcjs.link" {
		t.Fatalf("curURL = %q", a.curURL)
	}
	if a.title != "Minecraft（mcjs.link）" {
		t.Fatalf("title = %q", a.title)
	}
	if !strings.Contains(a.raw, "about:mcjs-open") || !strings.Contains(a.raw, "WebGL2") {
		t.Fatal("gateway page is missing the Firefox handoff")
	}
	found := false
	for _, line := range a.doc.lines {
		for _, sp := range line.spans {
			if sp.st.href == "about:mcjs-open" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("gateway link was not laid out")
	}
	for _, u := range []string{"https://mcjs.link/", "https://www.mcjs.link/play", "https://mcjs-r2.pages.dev/assets/game.html"} {
		if !isMCJSURL(u) {
			t.Fatalf("%s should be detected as an Eaglercraft host", u)
		}
	}
	for _, u := range []string{"https://example.com/", "about:home", "https://notmcjs.link/"} {
		if isMCJSURL(u) {
			t.Fatalf("%s should not be detected as an Eaglercraft host", u)
		}
	}
}
