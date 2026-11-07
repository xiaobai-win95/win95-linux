package desktop

import (
	"strings"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestPageJavaScriptDOMAndClick(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:test"}
	raw := `<!DOCTYPE html><html><body>
<p id="x" style="color:#C00000">0</p>
<button id="b" onclick="bump()">加一</button>
<p id="hidden" style="display:none">看不见</p>
<script>
var n = 0;
function bump() {
  n++;
  document.getElementById("x").textContent = "点击 " + n + " 次";
}
document.getElementById("x").textContent = "初始";
</script>
</body></html>`
	a.raw = raw
	a.kind = pageHTML
	a.startPageDOM()
	if a.js == nil {
		t.Fatal("JS runtime was not created")
	}
	if got := textContentOf(findID(a.dom, "x")); got != "初始" {
		t.Fatalf("script did not run: %q", got)
	}
	doc := layoutDOM(a.dom, 600)
	foundColored := false
	for _, line := range doc.lines {
		for _, sp := range line.spans {
			if strings.Contains(sp.text, "初") && sp.st.col == 0xC00000 && sp.st.hasCol {
				foundColored = true
			}
		}
	}
	if !foundColored {
		t.Fatal("inline color style was not applied to layout")
	}
	if a.js.dispatchClick(findID(a.dom, "b"), 10, 10) {
		t.Fatal("click should not be cancelled")
	}
	if got := textContentOf(findID(a.dom, "x")); got != "点击 1 次" {
		t.Fatalf("click handler did not run: %q", got)
	}
	hidden := findID(a.dom, "hidden")
	if !styleHidden(hidden) {
		t.Fatal("display:none was not detected")
	}
	layout := layoutDOM(a.dom, 600)
	for _, line := range layout.lines {
		for _, sp := range line.spans {
			if strings.Contains(sp.text, "看不见") {
				t.Fatal("hidden element leaked into layout")
			}
		}
	}
}

func TestPageJavaScriptWindowIsGlobal(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:global"}
	a.raw = `<html><body><p id="v"></p><script>
window.helperValue = 7;
document.getElementById("v").textContent =
  "v" + Win95.version + "|" + Win95.domBridge + "|" + helperValue;
</script></body></html>`
	a.kind = pageHTML
	a.startPageDOM()
	if a.js == nil {
		t.Fatal("JS runtime missing")
	}
	if len(a.js.errors) > 0 {
		t.Fatalf("script errors: %v", a.js.errors)
	}
	if got := textContentOf(findID(a.dom, "v")); got != "v26.2|26.2|7" {
		t.Fatalf("window/global binding = %q, want v26.2|26.2|7", got)
	}
}

func TestDemoPageRunsWithoutErrors(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:demo"}
	a.raw = demoPageHTML
	a.kind = pageHTML
	a.startPageDOM()
	if a.js == nil {
		t.Fatal("JS runtime missing")
	}
	if len(a.js.errors) > 0 {
		t.Fatalf("demo page script errors: %v", a.js.errors)
	}
	if got := textContentOf(findID(a.dom, "ver")); !strings.Contains(got, "26.2") || !strings.Contains(got, "goja") {
		t.Fatalf("demo version line = %q", got)
	}
}

func TestPageJavaScriptTimer(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &BrowserApp{d: d, visited: map[string]bool{}, histIdx: -1, curURL: "about:timer"}
	a.raw = `<html><body><p id="t">0</p><script>
var i = 0;
setInterval(function(){ i++; document.getElementById("t").textContent = String(i); }, 100);
</script></body></html>`
	a.kind = pageHTML
	a.startPageDOM()
	if a.js == nil {
		t.Fatal("JS runtime missing")
	}
	a.lastClient = ui.Rect{W: 800, H: 600}
	a.Animate(d, nil, time.Now().Add(250*time.Millisecond))
	if got := textContentOf(findID(a.dom, "t")); got == "0" || got == "" {
		t.Fatalf("timer did not advance: %q", got)
	}
}
