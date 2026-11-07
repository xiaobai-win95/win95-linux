package desktop

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

//go:embed browser_runtime.js
var browserRuntimeJS string

const (
	jsScriptTimeout = 3 * time.Second
	jsMaxLogLines   = 200
	jsMaxAlerts     = 10
)

type jsTimer struct {
	fn    goja.Callable
	due   time.Time
	every time.Duration
}

// pageJS holds the JavaScript runtime and the live DOM for one page.
type pageJS struct {
	a    *BrowserApp
	vm   *goja.Runtime
	root *xhtml.Node
	body *xhtml.Node

	wrappers map[*xhtml.Node]*goja.Object
	reverse  map[*goja.Object]*xhtml.Node
	styles   map[*xhtml.Node]*goja.Object
	inline   map[*xhtml.Node]map[string]goja.Callable
	executed map[*xhtml.Node]bool

	listeners    map[*xhtml.Node]map[string][]goja.Callable
	docListeners map[string][]goja.Callable
	winListeners map[string][]goja.Callable

	timers    map[int64]*jsTimer
	nextTimer int64

	alerts  []string
	logs    []string
	errors  []string
	dirty   bool
	closed  bool
	wrote   int
	curNode *xhtml.Node

	window, document, location, console *goja.Object
}

func newPageJS(a *BrowserApp, root *xhtml.Node, pageURL string) *pageJS {
	js := &pageJS{
		a:            a,
		vm:           goja.New(),
		root:         root,
		body:         findBody(root),
		wrappers:     make(map[*xhtml.Node]*goja.Object),
		reverse:      make(map[*goja.Object]*xhtml.Node),
		styles:       make(map[*xhtml.Node]*goja.Object),
		inline:       make(map[*xhtml.Node]map[string]goja.Callable),
		executed:     make(map[*xhtml.Node]bool),
		listeners:    make(map[*xhtml.Node]map[string][]goja.Callable),
		docListeners: make(map[string][]goja.Callable),
		winListeners: make(map[string][]goja.Callable),
		timers:       make(map[int64]*jsTimer),
	}
	if js.body == nil {
		js.body = root
	}
	js.install(pageURL)
	if err := js.runSource(browserDependencySource("runtime.js", browserRuntimeJS), "runtime.js"); err != nil {
		js.recordError(err.Error())
	}
	if err := js.runSource(browserDependencySource("dom-bridge.js", browserDOMBridgeJS), "dom-bridge.js"); err != nil {
		js.recordError(err.Error())
	}
	js.runScripts()
	js.fireEvent("DOMContentLoaded")
	js.fireWindow("DOMContentLoaded")
	js.fireWindow("load")
	return js
}

func (js *pageJS) install(pageURL string) {
	vm := js.vm
	// In a browser window IS the global object, so window.foo assignments
	// must also create the bare global foo that page scripts expect.
	win := vm.GlobalObject()
	doc := vm.NewObject()
	js.window, js.document = win, doc

	// window / document lifecycle methods.
	js.setNodeEvents(win, nil)
	js.setNodeEvents(doc, nil)
	win.Set("document", doc)
	win.Set("window", win)
	win.Set("self", win)
	win.Set("top", win)
	win.Set("parent", win)
	win.Set("name", "")
	win.Set("closed", false)
	win.Set("innerWidth", js.viewportW())
	win.Set("innerHeight", js.viewportH())
	win.Set("devicePixelRatio", 1)
	win.Set("scrollX", 0)
	win.Set("scrollY", 0)

	nav := vm.NewObject()
	nav.Set("userAgent", "Win95LinuxBrowser/26.2 (ARM64; Windows 95 Linux)")
	nav.Set("appName", "Netscape")
	nav.Set("appVersion", "26.2 (Win95Linux)")
	nav.Set("platform", "Windows 95 Linux ARM64")
	nav.Set("language", "zh-CN")
	nav.Set("languages", vm.NewArray("zh-CN", "zh", "en"))
	win.Set("navigator", nav)
	scr := vm.NewObject()
	scr.Set("width", js.viewportW())
	scr.Set("height", js.viewportH())
	scr.Set("availWidth", js.viewportW())
	scr.Set("availHeight", js.viewportH())
	scr.Set("colorDepth", 32)
	scr.Set("pixelDepth", 32)
	win.Set("screen", scr)

	con := vm.NewObject()
	consoleLog := func(label string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			var parts []string
			for _, v := range call.Arguments {
				parts = append(parts, js.valueText(v))
			}
			js.log(label + strings.Join(parts, " "))
			return goja.Undefined()
		}
	}
	con.Set("log", consoleLog(""))
	con.Set("info", consoleLog("[信息] "))
	con.Set("warn", consoleLog("[警告] "))
	con.Set("error", consoleLog("[错误] "))
	con.Set("debug", consoleLog("[调试] "))
	js.console = con
	win.Set("console", con)

	loc := vm.NewObject()
	js.location = loc
	loc.Set("href", pageURL)
	loc.Set("protocol", urlProtocol(pageURL))
	loc.Set("host", "")
	loc.Set("hostname", "")
	loc.Set("pathname", pageURL)
	loc.Set("search", "")
	loc.Set("hash", "")
	loc.Set("reload", func(goja.FunctionCall) goja.Value {
		js.a.reload()
		return goja.Undefined()
	})
	loc.Set("assign", func(call goja.FunctionCall) goja.Value {
		js.a.Navigate(call.Argument(0).String())
		return goja.Undefined()
	})
	loc.Set("replace", func(call goja.FunctionCall) goja.Value {
		js.a.Navigate(call.Argument(0).String())
		return goja.Undefined()
	})
	loc.Set("toString", func(goja.FunctionCall) goja.Value { return js.vm.ToValue(js.a.curURL) })
	win.Set("location", loc)

	js.defineDocNode(doc)
	win.DefineAccessorProperty("onload",
		js.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Null() }),
		js.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
				js.addWinListener("load", fn)
			}
			return goja.Undefined()
		}), goja.FLAG_TRUE, goja.FLAG_TRUE)

	alertFn := func(call goja.FunctionCall) goja.Value {
		js.queueAlert(js.valueText(call.Argument(0)))
		return goja.Undefined()
	}
	confirmFn := func(call goja.FunctionCall) goja.Value {
		js.log("[提示] 页面调用了 confirm()，本浏览器返回 false")
		return js.vm.ToValue(false)
	}
	promptFn := func(call goja.FunctionCall) goja.Value {
		js.log("[提示] 页面调用了 prompt()，本浏览器返回 null")
		return goja.Null()
	}
	timeoutFn := func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return js.vm.ToValue(0)
		}
		return js.vm.ToValue(js.addTimer(fn, call.Argument(1).ToInteger(), 0))
	}
	intervalFn := func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return js.vm.ToValue(0)
		}
		ms := call.Argument(1).ToInteger()
		if ms < 50 {
			ms = 50
		}
		return js.vm.ToValue(js.addTimer(fn, ms, time.Duration(ms)*time.Millisecond))
	}
	clearFn := func(call goja.FunctionCall) goja.Value {
		js.clearTimer(call.Argument(0).ToInteger())
		return goja.Undefined()
	}
	base64Encode := func(call goja.FunctionCall) goja.Value {
		return js.vm.ToValue(base64EncodeString(call.Argument(0).String()))
	}
	base64Decode := func(call goja.FunctionCall) goja.Value {
		if s, err := base64DecodeString(call.Argument(0).String()); err == nil {
			return js.vm.ToValue(s)
		}
		return js.vm.ToValue("")
	}
	setGlobal := func(name string, fn func(goja.FunctionCall) goja.Value) {
		win.Set(name, fn)
		_ = vm.Set(name, fn)
	}
	setGlobal("alert", alertFn)
	setGlobal("confirm", confirmFn)
	setGlobal("prompt", promptFn)
	setGlobal("setTimeout", timeoutFn)
	setGlobal("setInterval", intervalFn)
	setGlobal("clearTimeout", clearFn)
	setGlobal("clearInterval", clearFn)
	setGlobal("btoa", base64Encode)
	setGlobal("atob", base64Decode)

	_ = vm.Set("window", win)
	_ = vm.Set("self", win)
	_ = vm.Set("document", doc)
	_ = vm.Set("console", con)
	_ = vm.Set("location", loc)
	_ = vm.Set("navigator", nav)
	_ = vm.Set("screen", scr)
	_ = vm.Set("innerWidth", js.viewportW())
	_ = vm.Set("innerHeight", js.viewportH())
}

func base64EncodeString(s string) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var b strings.Builder
	data := []byte(s)
	for i := 0; i < len(data); i += 3 {
		var n uint32
		rem := len(data) - i
		n = uint32(data[i]) << 16
		if rem > 1 {
			n |= uint32(data[i+1]) << 8
		}
		if rem > 2 {
			n |= uint32(data[i+2])
		}
		b.WriteByte(table[(n>>18)&63])
		b.WriteByte(table[(n>>12)&63])
		if rem > 1 {
			b.WriteByte(table[(n>>6)&63])
		} else {
			b.WriteByte('=')
		}
		if rem > 2 {
			b.WriteByte(table[n&63])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}

func base64DecodeString(s string) (string, error) {
	vals := make(map[byte]byte, 64)
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < len(table); i++ {
		vals[table[i]] = byte(i)
	}
	s = strings.TrimSpace(s)
	var out []byte
	var buf uint32
	bits := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			break
		}
		v, ok := vals[s[i]]
		if !ok {
			return "", fmt.Errorf("非法 base64 字符")
		}
		buf = (buf << 6) | uint32(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buf>>uint(bits)))
		}
	}
	return string(out), nil
}

func (js *pageJS) viewportW() int {
	if js.a.lastClient.W > 0 {
		return js.a.lastClient.W
	}
	return 800
}

func (js *pageJS) viewportH() int {
	if js.a.lastClient.H > 0 {
		return js.a.lastClient.H
	}
	return 600
}

func urlProtocol(u string) string {
	if i := strings.Index(u, ":"); i > 0 {
		return u[:i] + ":"
	}
	return ""
}

func (js *pageJS) defineDocNode(doc *goja.Object) {
	doc.Set("nodeType", 9)
	doc.Set("nodeName", "#document")
	doc.Set("readyState", "loading")
	doc.Set("cookie", "")
	doc.Set("charset", "UTF-8")
	doc.Set("characterSet", "UTF-8")
	doc.Set("URL", js.a.curURL)
	doc.Set("documentURI", js.a.curURL)
	doc.Set("referrer", "")
	doc.Set("hidden", false)
	doc.Set("visibilityState", "visible")

	js.defineTextProp(doc, "title", func() string {
		if t := findTitle(js.root); t != "" {
			return t
		}
		return js.a.title
	}, func(s string) {
		if n := findFirstElement(js.root, "title"); n != nil {
			setTextContent(n, s)
		} else if head := findFirstElement(js.root, "head"); head != nil {
			tn := &xhtml.Node{Type: xhtml.ElementNode, Data: "title", DataAtom: atom.Title}
			tn.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: s})
			head.AppendChild(tn)
		}
		js.a.title = s
		js.markDirty()
	})
	js.defineNodeProp(doc, "documentElement", func() *xhtml.Node { return findFirstElement(js.root, "html") })
	js.defineNodeProp(doc, "body", func() *xhtml.Node { return js.body })
	js.defineNodeProp(doc, "head", func() *xhtml.Node { return findFirstElement(js.root, "head") })

	doc.Set("getElementById", func(call goja.FunctionCall) goja.Value {
		id := call.Argument(0).String()
		if n := findID(js.root, id); n != nil {
			return js.wrap(n)
		}
		return goja.Null()
	})
	doc.Set("getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(findByTag(js.root, strings.ToLower(call.Argument(0).String())))
	})
	doc.Set("getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(findByClass(js.root, call.Argument(0).String()))
	})
	doc.Set("querySelector", func(call goja.FunctionCall) goja.Value {
		if n := queryOne(js.root, call.Argument(0).String()); n != nil {
			return js.wrap(n)
		}
		return goja.Null()
	})
	doc.Set("querySelectorAll", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(queryAll(js.root, call.Argument(0).String()))
	})
	doc.Set("createElement", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if tag == "" {
			tag = "div"
		}
		n := &xhtml.Node{Type: xhtml.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
		return js.wrap(n)
	})
	doc.Set("createTextNode", func(call goja.FunctionCall) goja.Value {
		n := &xhtml.Node{Type: xhtml.TextNode, Data: call.Argument(0).String()}
		return js.wrap(n)
	})
	doc.Set("createDocumentFragment", func(goja.FunctionCall) goja.Value {
		n := &xhtml.Node{Type: xhtml.DocumentNode}
		return js.wrap(n)
	})
	doc.Set("write", func(call goja.FunctionCall) goja.Value {
		var b strings.Builder
		for _, v := range call.Arguments {
			b.WriteString(js.valueText(v))
		}
		js.writeHTML(b.String())
		return goja.Undefined()
	})
	doc.Set("writeln", func(call goja.FunctionCall) goja.Value {
		var b strings.Builder
		for _, v := range call.Arguments {
			b.WriteString(js.valueText(v))
		}
		b.WriteString("\n")
		js.writeHTML(b.String())
		return goja.Undefined()
	})
	doc.Set("open", func(goja.FunctionCall) goja.Value { return doc })
	doc.Set("close", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	doc.Set("hasFocus", func(goja.FunctionCall) goja.Value { return js.vm.ToValue(true) })
}

// wrap returns a JS object for a DOM node. Wrappers are cached so identity
// comparisons such as a.parentNode === b work inside page scripts.
func (js *pageJS) wrap(n *xhtml.Node) goja.Value {
	if n == nil {
		return goja.Null()
	}
	if o, ok := js.wrappers[n]; ok {
		return o
	}
	o := js.vm.NewObject()
	js.wrappers[n] = o
	js.reverse[o] = n
	js.defineNodeObject(o, n)
	return o
}

func (js *pageJS) defineNodeObject(o *goja.Object, n *xhtml.Node) {
	vm := js.vm
	nodeType := 1
	nodeName := strings.ToUpper(n.Data)
	if n.Type == xhtml.TextNode {
		nodeType = 3
		nodeName = "#text"
	} else if n.Type != xhtml.ElementNode {
		nodeType = 11
		nodeName = "#fragment"
	}
	o.Set("nodeType", nodeType)
	o.Set("nodeName", nodeName)
	if n.Type == xhtml.ElementNode {
		o.Set("tagName", nodeName)
	} else {
		o.Set("tagName", "")
	}
	js.defineBoolProp(o, "hidden", func() bool { return hasAttr(n, "hidden") }, func(v bool) { toggleAttr(n, "hidden", v); js.markDirty() })
	js.defineTextProp(o, "id", func() string { return attr(n, "id") }, func(v string) { setAttr(n, "id", v); js.markDirty() })
	js.defineTextProp(o, "className", func() string { return attr(n, "class") }, func(v string) { setAttr(n, "class", v); js.markDirty() })
	js.defineTextProp(o, "title", func() string { return attr(n, "title") }, func(v string) { setAttr(n, "title", v); js.markDirty() })
	js.defineTextProp(o, "href", func() string { return attr(n, "href") }, func(v string) { setAttr(n, "href", v); js.markDirty() })
	js.defineTextProp(o, "src", func() string { return attr(n, "src") }, func(v string) { setAttr(n, "src", v); js.markDirty() })
	js.defineTextProp(o, "value", func() string { return attr(n, "value") }, func(v string) { setAttr(n, "value", v); js.markDirty() })
	js.defineBoolProp(o, "checked", func() bool { return hasAttr(n, "checked") }, func(v bool) { toggleAttr(n, "checked", v); js.markDirty() })
	js.defineBoolProp(o, "disabled", func() bool { return hasAttr(n, "disabled") }, func(v bool) { toggleAttr(n, "disabled", v); js.markDirty() })
	js.defineTextProp(o, "textContent", func() string { return textContentOf(n) }, func(v string) { setTextContent(n, v); js.markDirty() })
	js.defineTextProp(o, "innerText", func() string { return textContentOf(n) }, func(v string) { setTextContent(n, v); js.markDirty() })
	js.defineTextProp(o, "innerHTML", func() string { return innerHTMLOf(n) }, func(v string) { js.setInnerHTML(n, v) })
	js.defineTextProp(o, "outerHTML", func() string { return outerHTMLOf(n) }, nil)
	js.defineTextProp(o, "nodeValue", func() string {
		if n.Type == xhtml.TextNode {
			return n.Data
		}
		return ""
	}, func(v string) {
		if n.Type == xhtml.TextNode {
			n.Data = v
			js.markDirty()
		}
	})
	js.defineNodeProp(o, "parentNode", func() *xhtml.Node { return n.Parent })
	js.defineNodeProp(o, "parentElement", func() *xhtml.Node {
		if n.Parent != nil && n.Parent.Type == xhtml.ElementNode {
			return n.Parent
		}
		return nil
	})
	js.defineNodeProp(o, "firstChild", func() *xhtml.Node { return n.FirstChild })
	js.defineNodeProp(o, "lastChild", func() *xhtml.Node { return n.LastChild })
	js.defineNodeProp(o, "nextSibling", func() *xhtml.Node { return n.NextSibling })
	js.defineNodeProp(o, "previousSibling", func() *xhtml.Node { return n.PrevSibling })
	js.defineNodeProp(o, "firstElementChild", func() *xhtml.Node { return firstElementChild(n) })
	js.defineNodeProp(o, "lastElementChild", func() *xhtml.Node { return lastElementChild(n) })
	js.defineNodeProp(o, "nextElementSibling", func() *xhtml.Node { return nextElementSibling(n) })
	js.defineNodeProp(o, "previousElementSibling", func() *xhtml.Node { return prevElementSibling(n) })
	o.DefineAccessorProperty("children",
		vm.ToValue(func(goja.FunctionCall) goja.Value { return js.arrayOf(elementChildren(n)) }),
		nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
	o.DefineAccessorProperty("childNodes",
		vm.ToValue(func(goja.FunctionCall) goja.Value { return js.arrayOf(allChildren(n)) }),
		nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
	o.Set("childElementCount", len(elementChildren(n)))
	o.Set("classList", js.classListObject(n))

	o.Set("getAttribute", func(call goja.FunctionCall) goja.Value {
		name := strings.ToLower(call.Argument(0).String())
		for _, a := range n.Attr {
			if a.Key == name {
				return vm.ToValue(a.Val)
			}
		}
		return goja.Null()
	})
	o.Set("setAttribute", func(call goja.FunctionCall) goja.Value {
		setAttr(n, strings.ToLower(call.Argument(0).String()), call.Argument(1).String())
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("hasAttribute", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(hasAttr(n, strings.ToLower(call.Argument(0).String())))
	})
	o.Set("removeAttribute", func(call goja.FunctionCall) goja.Value {
		removeAttr(n, strings.ToLower(call.Argument(0).String()))
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("appendChild", func(call goja.FunctionCall) goja.Value {
		child := js.nodeArg(call.Argument(0))
		if child != nil {
			js.adopt(n, child)
			js.markDirty()
		}
		return call.Argument(0)
	})
	o.Set("insertBefore", func(call goja.FunctionCall) goja.Value {
		child := js.nodeArg(call.Argument(0))
		ref := js.nodeArg(call.Argument(1))
		if child != nil {
			js.adoptBefore(n, child, ref)
			js.markDirty()
		}
		return call.Argument(0)
	})
	o.Set("removeChild", func(call goja.FunctionCall) goja.Value {
		child := js.nodeArg(call.Argument(0))
		if child != nil && child.Parent == n {
			n.RemoveChild(child)
			js.markDirty()
		}
		return call.Argument(0)
	})
	o.Set("replaceChild", func(call goja.FunctionCall) goja.Value {
		child := js.nodeArg(call.Argument(0))
		old := js.nodeArg(call.Argument(1))
		if child != nil && old != nil && old.Parent == n {
			js.adoptBefore(n, child, old)
			n.RemoveChild(old)
			js.markDirty()
		}
		return call.Argument(1)
	})
	o.Set("remove", func(goja.FunctionCall) goja.Value {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
			js.markDirty()
		}
		return goja.Undefined()
	})
	o.Set("getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(findByTag(n, strings.ToLower(call.Argument(0).String())))
	})
	o.Set("getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(findByClass(n, call.Argument(0).String()))
	})
	o.Set("querySelector", func(call goja.FunctionCall) goja.Value {
		if m := queryOne(n, call.Argument(0).String()); m != nil {
			return js.wrap(m)
		}
		return goja.Null()
	})
	o.Set("querySelectorAll", func(call goja.FunctionCall) goja.Value {
		return js.arrayOf(queryAll(n, call.Argument(0).String()))
	})
	o.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		js.addNodeListener(n, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	o.Set("removeEventListener", func(call goja.FunctionCall) goja.Value {
		js.removeNodeListener(n, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	o.Set("dispatchEvent", func(call goja.FunctionCall) goja.Value {
		js.dispatchNodeEvent(n, "click", 0, 0)
		return goja.Undefined()
	})
	o.Set("click", func(goja.FunctionCall) goja.Value {
		js.dispatchNodeEvent(n, "click", 0, 0)
		return goja.Undefined()
	})
	o.Set("focus", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("blur", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("insertAdjacentHTML", func(call goja.FunctionCall) goja.Value {
		pos := strings.ToLower(call.Argument(0).String())
		nodes := parseFragment(call.Argument(1).String())
		for i := len(nodes) - 1; i >= 0; i-- {
			child := nodes[i]
			switch pos {
			case "beforeend":
				n.AppendChild(child)
			case "afterbegin":
				n.InsertBefore(child, n.FirstChild)
			case "beforebegin":
				if n.Parent != nil {
					n.Parent.InsertBefore(child, n)
				}
			case "afterend":
				if n.Parent != nil {
					n.Parent.InsertBefore(child, n.NextSibling)
				}
			}
		}
		js.markDirty()
		return goja.Undefined()
	})
	o.DefineAccessorProperty("style",
		vm.ToValue(func(goja.FunctionCall) goja.Value { return js.styleObject(n) }),
		nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
}

func (js *pageJS) defineNodeProp(o *goja.Object, name string, get func() *xhtml.Node) {
	o.DefineAccessorProperty(name,
		js.vm.ToValue(func(goja.FunctionCall) goja.Value {
			return js.wrap(get())
		}), nil, goja.FLAG_TRUE, goja.FLAG_TRUE)
}

func (js *pageJS) defineTextProp(o *goja.Object, name string, get func() string, set func(string)) {
	var setter goja.Value
	if set != nil {
		setter = js.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			set(call.Argument(0).String())
			return goja.Undefined()
		})
	}
	o.DefineAccessorProperty(name, js.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return js.vm.ToValue(get())
	}), setter, goja.FLAG_TRUE, goja.FLAG_TRUE)
}

func (js *pageJS) defineBoolProp(o *goja.Object, name string, get func() bool, set func(bool)) {
	o.DefineAccessorProperty(name, js.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return js.vm.ToValue(get())
	}), js.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		set(call.Argument(0).ToBoolean())
		return goja.Undefined()
	}), goja.FLAG_TRUE, goja.FLAG_TRUE)
}

func (js *pageJS) arrayOf(nodes []*xhtml.Node) goja.Value {
	items := make([]interface{}, len(nodes))
	for i, n := range nodes {
		items[i] = js.wrap(n)
	}
	return js.vm.NewArray(items...)
}

func (js *pageJS) nodeArg(v goja.Value) *xhtml.Node {
	o, ok := v.(*goja.Object)
	if !ok {
		return nil
	}
	return js.reverse[o]
}

func (js *pageJS) adopt(parent, child *xhtml.Node) {
	if child.Parent != nil {
		child.Parent.RemoveChild(child)
	}
	parent.AppendChild(child)
}

func (js *pageJS) adoptBefore(parent, child, ref *xhtml.Node) {
	if child.Parent != nil {
		child.Parent.RemoveChild(child)
	}
	if ref == nil {
		parent.AppendChild(child)
		return
	}
	parent.InsertBefore(child, ref)
}

func (js *pageJS) setInnerHTML(n *xhtml.Node, s string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		c = next
	}
	for _, child := range parseFragment(s) {
		n.AppendChild(child)
		js.executeSubtreeScripts(child)
	}
	js.markDirty()
}

func (js *pageJS) writeHTML(s string) {
	if js.closed || js.wrote >= 100 || len(s) > 256*1024 {
		return
	}
	js.wrote++
	nodes := parseFragment(s)
	if len(nodes) == 0 {
		return
	}
	parent := js.body
	pos := (*xhtml.Node)(nil)
	if js.curNode != nil && js.curNode.Parent != nil {
		parent = js.curNode.Parent
		pos = js.curNode
	}
	if parent == nil {
		return
	}
	for _, child := range nodes {
		if pos != nil {
			parent.InsertBefore(child, pos.NextSibling)
		} else {
			parent.AppendChild(child)
		}
		pos = child
		js.executeSubtreeScripts(child)
	}
	js.markDirty()
}

func (js *pageJS) runScripts() {
	var nodes []*xhtml.Node
	collectNodes(js.root, func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "script" {
			nodes = append(nodes, n)
		}
	})
	for _, n := range nodes {
		js.execScript(n)
	}
}

func (js *pageJS) executeSubtreeScripts(n *xhtml.Node) {
	if n.Type == xhtml.ElementNode && n.Data == "script" {
		js.execScript(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		js.executeSubtreeScripts(c)
	}
}

func (js *pageJS) execScript(n *xhtml.Node) {
	if js.closed || js.executed[n] {
		return
	}
	js.executed[n] = true
	typ := strings.ToLower(strings.TrimSpace(attr(n, "type")))
	if typ != "" && typ != "text/javascript" && typ != "application/javascript" && typ != "text/ecmascript" && typ != "application/ecmascript" {
		if strings.Contains(typ, "module") {
			js.recordError("不支持 JavaScript 模块脚本")
		}
		return
	}
	code := textContentOf(n)
	if src := attr(n, "src"); src != "" {
		ext, err := js.loadScript(src)
		if err != nil {
			js.recordError("外部脚本 " + src + "：" + err.Error())
			return
		}
		code = ext
	}
	if strings.TrimSpace(code) == "" {
		return
	}
	if len(code) > 512*1024 {
		js.recordError("脚本超过 512 KB，已跳过")
		return
	}
	prev := js.curNode
	js.curNode = n
	err := js.runSource(code, attr(n, "src"))
	js.curNode = prev
	if err != nil {
		js.recordError(err.Error())
	}
}

func (js *pageJS) loadScript(src string) (string, error) {
	target := resolveURL(js.a.curURL, src)
	lower := strings.ToLower(target)
	if !strings.HasPrefix(lower, "file://") {
		return "", fmt.Errorf("仅支持本地 file:// 外部脚本")
	}
	p := strings.TrimPrefix(target, "file://")
	p = strings.TrimPrefix(p, "localhost")
	if u, err := filepath.Abs(p); err == nil {
		p = u
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if len(data) > 512*1024 {
		return "", fmt.Errorf("文件过大")
	}
	return string(data), nil
}

func (js *pageJS) runSource(code, label string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("运行时崩溃: %v", r)
		}
		js.vm.ClearInterrupt()
	}()
	timer := time.AfterFunc(jsScriptTimeout, func() {
		js.vm.Interrupt("script timeout")
	})
	defer timer.Stop()
	if _, err = js.vm.RunString(code); err != nil {
		if label != "" {
			return fmt.Errorf("%s: %w", label, err)
		}
		return err
	}
	return nil
}

func (js *pageJS) recordError(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	js.errors = append(js.errors, msg)
	if len(js.errors) > 20 {
		js.errors = js.errors[len(js.errors)-20:]
	}
	js.log("[JS 错误] " + msg)
	js.a.setJSStatus("JavaScript 错误：" + msg)
}

func (js *pageJS) log(s string) {
	line := s
	if len(line) > 500 {
		line = line[:500] + "…"
	}
	js.logs = append(js.logs, line)
	if len(js.logs) > jsMaxLogLines {
		js.logs = js.logs[len(js.logs)-jsMaxLogLines:]
	}
	js.a.jsLastLog = line
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

func (js *pageJS) valueText(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	if o, ok := v.(*goja.Object); ok {
		if n := js.reverse[o]; n != nil {
			if n.Type == xhtml.TextNode {
				return n.Data
			}
			return "[object HTML" + titleCase(n.Data) + "Element]"
		}
	}
	return v.String()
}

func (js *pageJS) queueAlert(msg string) {
	js.alerts = append(js.alerts, msg)
	if len(js.alerts) > jsMaxAlerts {
		js.alerts = js.alerts[len(js.alerts)-jsMaxAlerts:]
	}
	a := js.a
	a.d.Post(func() {
		if a.js == js {
			js.flushAlerts()
		}
	})
}

func (js *pageJS) flushAlerts() {
	if len(js.alerts) == 0 {
		return
	}
	text := strings.Join(js.alerts, "\n\n")
	js.alerts = nil
	js.a.d.MessageBox("网页提示", text, IconInfo, []string{"确定"}, nil)
}

func (js *pageJS) addTimer(fn goja.Callable, delayMs int64, every time.Duration) int64 {
	if delayMs < 0 {
		delayMs = 0
	}
	if delayMs > 3600000 {
		delayMs = 3600000
	}
	js.nextTimer++
	id := js.nextTimer
	js.timers[id] = &jsTimer{fn: fn, due: time.Now().Add(time.Duration(delayMs) * time.Millisecond), every: every}
	return id
}

func (js *pageJS) clearTimer(id int64) {
	delete(js.timers, id)
}

func (js *pageJS) addNodeListener(n *xhtml.Node, typ string, v goja.Value) {
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return
	}
	m := js.listeners[n]
	if m == nil {
		m = make(map[string][]goja.Callable)
		js.listeners[n] = m
	}
	m[typ] = append(m[typ], fn)
}

func (js *pageJS) removeNodeListener(n *xhtml.Node, typ string, v goja.Value) {
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	list := js.listeners[n][typ]
	for i, f := range list {
		if &f == &fn {
			js.listeners[n][typ] = append(list[:i], list[i+1:]...)
			return
		}
	}
}

func (js *pageJS) addWinListener(typ string, fn goja.Callable) {
	typ = strings.ToLower(typ)
	js.winListeners[typ] = append(js.winListeners[typ], fn)
}

func (js *pageJS) setNodeEvents(o *goja.Object, n *xhtml.Node) {
	o.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			return goja.Undefined()
		}
		typ := strings.ToLower(call.Argument(0).String())
		if n == nil {
			if o == js.window {
				js.winListeners[typ] = append(js.winListeners[typ], fn)
			} else {
				js.docListeners[typ] = append(js.docListeners[typ], fn)
			}
			return goja.Undefined()
		}
		js.addNodeListener(n, typ, call.Argument(1))
		return goja.Undefined()
	})
	o.Set("removeEventListener", func(call goja.FunctionCall) goja.Value {
		typ := strings.ToLower(call.Argument(0).String())
		if n == nil {
			js.docListeners[typ] = nil
			return goja.Undefined()
		}
		js.removeNodeListener(n, typ, call.Argument(1))
		return goja.Undefined()
	})
}

func (js *pageJS) nodeClickable(n *xhtml.Node) bool {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type != xhtml.ElementNode {
			continue
		}
		if attr(cur, "onclick") != "" {
			return true
		}
		if m := js.listeners[cur]; m != nil && len(m["click"]) > 0 {
			return true
		}
	}
	return false
}

// dispatchClick runs inline onclick and click listeners with bubbling.
// It returns true when the default action should be cancelled.
func (js *pageJS) dispatchClick(n *xhtml.Node, x, y int) bool {
	if js == nil || js.closed || n == nil {
		return false
	}
	prevented := false
	stopped := false
	ev := js.vm.NewObject()
	ev.Set("type", "click")
	ev.Set("clientX", x)
	ev.Set("clientY", y)
	ev.Set("pageX", x)
	ev.Set("pageY", y)
	ev.Set("button", 0)
	ev.Set("defaultPrevented", false)
	ev.Set("cancelBubble", false)
	ev.Set("target", js.wrap(n))
	ev.Set("preventDefault", func(goja.FunctionCall) goja.Value {
		prevented = true
		ev.Set("defaultPrevented", true)
		return goja.Undefined()
	})
	stop := func(goja.FunctionCall) goja.Value {
		stopped = true
		ev.Set("cancelBubble", true)
		return goja.Undefined()
	}
	ev.Set("stopPropagation", stop)
	ev.Set("stopImmediatePropagation", stop)
	var path []*xhtml.Node
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == xhtml.ElementNode {
			path = append(path, cur)
		}
	}
	for _, cur := range path {
		if stopped {
			break
		}
		ev.Set("currentTarget", js.wrap(cur))
		if code := attr(cur, "onclick"); code != "" {
			handler := js.inlineHandler(cur, "onclick", code)
			if handler != nil {
				ret, err := js.call(handler, js.wrap(cur), ev)
				if err != nil {
					js.recordError("onclick：" + err.Error())
				} else if b, ok := ret.Export().(bool); ok && !b {
					prevented = true
				}
			}
		}
		if m := js.listeners[cur]; m != nil {
			for _, fn := range append([]goja.Callable(nil), m["click"]...) {
				if _, err := js.call(fn, js.wrap(cur), ev); err != nil {
					js.recordError("click 事件：" + err.Error())
				}
			}
		}
	}
	if !stopped {
		ev.Set("currentTarget", js.document)
		for _, fn := range append([]goja.Callable(nil), js.docListeners["click"]...) {
			if _, err := js.call(fn, js.document, ev); err != nil {
				js.recordError("document click：" + err.Error())
			}
		}
		for _, fn := range append([]goja.Callable(nil), js.winListeners["click"]...) {
			if _, err := js.call(fn, js.window, ev); err != nil {
				js.recordError("window click：" + err.Error())
			}
		}
	}
	return prevented
}

func (js *pageJS) dispatchNodeEvent(n *xhtml.Node, typ string, x, y int) {
	if typ == "click" {
		js.dispatchClick(n, x, y)
	}
}

func (js *pageJS) inlineHandler(n *xhtml.Node, evName, code string) goja.Callable {
	m := js.inline[n]
	if m == nil {
		m = make(map[string]goja.Callable)
		js.inline[n] = m
	}
	if fn, ok := m[evName]; ok {
		return fn
	}
	v, err := js.vm.RunString("(function(event){" + code + "\n})")
	if err != nil {
		js.recordError("内联事件编译失败：" + err.Error())
		return nil
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return nil
	}
	m[evName] = fn
	return fn
}

func (js *pageJS) call(fn goja.Callable, this goja.Value, args ...goja.Value) (ret goja.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("回调崩溃: %v", r)
		}
		js.vm.ClearInterrupt()
	}()
	timer := time.AfterFunc(jsScriptTimeout, func() { js.vm.Interrupt("callback timeout") })
	defer timer.Stop()
	return fn(this, args...)
}

func (js *pageJS) fireEvent(typ string) {
	ev := js.simpleEvent(typ, js.document)
	for _, fn := range append([]goja.Callable(nil), js.docListeners[typ]...) {
		if _, err := js.call(fn, js.document, ev); err != nil {
			js.recordError(typ + "：" + err.Error())
		}
	}
}

func (js *pageJS) fireWindow(typ string) {
	ev := js.simpleEvent(typ, js.window)
	for _, fn := range append([]goja.Callable(nil), js.winListeners[typ]...) {
		if _, err := js.call(fn, js.window, ev); err != nil {
			js.recordError(typ + "：" + err.Error())
		}
	}
	if typ == "load" || typ == "DOMContentLoaded" {
		js.document.Set("readyState", "complete")
	}
}

func (js *pageJS) simpleEvent(typ string, target *goja.Object) *goja.Object {
	ev := js.vm.NewObject()
	ev.Set("type", typ)
	ev.Set("target", target)
	ev.Set("currentTarget", target)
	ev.Set("defaultPrevented", false)
	ev.Set("preventDefault", func(goja.FunctionCall) goja.Value {
		ev.Set("defaultPrevented", true)
		return goja.Undefined()
	})
	ev.Set("stopPropagation", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	return ev
}

func (js *pageJS) markDirty() {
	if js.closed {
		return
	}
	js.dirty = true
	js.a.d.Invalidate()
}

func (js *pageJS) stop() {
	if js.closed {
		return
	}
	js.closed = true
	js.timers = make(map[int64]*jsTimer)
	js.alerts = nil
}

func (js *pageJS) eval(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	v, err := js.vm.RunString(expr)
	if err != nil {
		return "", err
	}
	if v == nil {
		return "undefined", nil
	}
	return js.valueText(v), nil
}

func (js *pageJS) consoleText() string {
	var b strings.Builder
	if len(js.logs) > 0 {
		b.WriteString("控制台输出：\n")
		for _, line := range js.logs {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if len(js.errors) > 0 {
		b.WriteString("\n脚本错误：\n")
		for _, line := range js.errors {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

// BrowserApp.Animate drives timers and DOM re-layout for JavaScript pages.
func (a *BrowserApp) Animate(d *Desktop, w *Window, now time.Time) {
	js := a.js
	if js == nil || js.closed {
		return
	}
	changed := false
	for id, t := range js.timers {
		if now.Before(t.due) {
			continue
		}
		if t.every > 0 {
			t.due = now.Add(t.every)
		} else {
			delete(js.timers, id)
		}
		if _, err := js.call(t.fn, js.window, goja.Undefined()); err != nil {
			js.recordError("定时器：" + err.Error())
		}
		changed = true
	}
	if js.dirty {
		js.dirty = false
		a.rebuild()
		changed = true
	}
	if changed {
		d.Invalidate()
	}
}

func (a *BrowserApp) setJSStatus(msg string) {
	a.status = msg
	if len(a.jsHits) > 0 {
		a.jsHits = a.jsHits[:0]
	}
	a.d.Invalidate()
}

func (a *BrowserApp) stopPageJS() {
	if a.js != nil {
		a.js.stop()
		a.js = nil
	}
	a.dom = nil
}

func (a *BrowserApp) openJSConsole() {
	a.d.closeContext()
	text := "当前页面没有 JavaScript 运行时。"
	if a.js != nil {
		if t := a.js.consoleText(); t != "" {
			text = t
		} else {
			text = "控制台为空。可输入表达式执行，例如：document.title 或 2+3*4"
		}
	}
	a.d.MessageBox("JavaScript 控制台", text, IconInfo, []string{"求值…", "关闭"}, func(i int) {
		if i != 0 {
			return
		}
		a.promptJSEval()
	})
}

func (a *BrowserApp) promptJSEval() {
	a.d.InputBox("JavaScript 控制台", "输入要执行的 JavaScript 表达式：", "", func(code string, ok bool) {
		if !ok || a.js == nil {
			return
		}
		out, err := a.js.eval(code)
		if err != nil {
			a.d.MessageBox("JavaScript 错误", err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		a.d.MessageBox("JavaScript 结果", "> "+code+"\n\n"+out, IconInfo, []string{"再次求值…", "确定"}, func(i int) {
			if i == 0 {
				a.promptJSEval()
			}
		})
	})
}

// --- DOM tree helpers -------------------------------------------------------

func parseFragment(s string) []*xhtml.Node {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		return nil
	}
	return nodes
}

func collectNodes(n *xhtml.Node, fn func(*xhtml.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectNodes(c, fn)
	}
}

func findFirstElement(n *xhtml.Node, tag string) *xhtml.Node {
	if n == nil {
		return nil
	}
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if m := findFirstElement(c, tag); m != nil {
			return m
		}
	}
	return nil
}

func findID(n *xhtml.Node, id string) *xhtml.Node {
	if n == nil || id == "" {
		return nil
	}
	if n.Type == xhtml.ElementNode && attr(n, "id") == id {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if m := findID(c, id); m != nil {
			return m
		}
	}
	return nil
}

func findByTag(n *xhtml.Node, tag string) []*xhtml.Node {
	var out []*xhtml.Node
	if tag == "" {
		return out
	}
	collectNodes(n, func(m *xhtml.Node) {
		if m.Type != xhtml.ElementNode {
			return
		}
		if tag == "*" || m.Data == tag {
			out = append(out, m)
		}
	})
	return out
}

func findByClass(n *xhtml.Node, class string) []*xhtml.Node {
	var out []*xhtml.Node
	class = strings.TrimSpace(class)
	if class == "" {
		return out
	}
	collectNodes(n, func(m *xhtml.Node) {
		if m.Type == xhtml.ElementNode && hasClass(m, class) {
			out = append(out, m)
		}
	})
	return out
}

func hasClass(n *xhtml.Node, class string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

func queryOne(root *xhtml.Node, sel string) *xhtml.Node {
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens := strings.Fields(part)
		matches := []*xhtml.Node{root}
		for _, tok := range tokens {
			var next []*xhtml.Node
			seen := map[*xhtml.Node]bool{}
			for _, m := range matches {
				collectNodes(m, func(c *xhtml.Node) {
					if c == m || seen[c] {
						return
					}
					if matchSimple(c, tok) {
						seen[c] = true
						next = append(next, c)
					}
				})
			}
			matches = next
			if len(matches) == 0 {
				break
			}
		}
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return nil
}

func queryAll(root *xhtml.Node, sel string) []*xhtml.Node {
	var out []*xhtml.Node
	seen := map[*xhtml.Node]bool{}
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens := strings.Fields(part)
		matches := []*xhtml.Node{root}
		for _, tok := range tokens {
			var next []*xhtml.Node
			for _, m := range matches {
				collectNodes(m, func(c *xhtml.Node) {
					if c == m {
						return
					}
					if matchSimple(c, tok) {
						next = append(next, c)
					}
				})
			}
			matches = next
			if len(matches) == 0 {
				break
			}
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func matchSimple(n *xhtml.Node, sel string) bool {
	if n == nil || n.Type != xhtml.ElementNode || sel == "" {
		return false
	}
	if sel == "*" {
		return true
	}
	tag := sel
	if i := strings.IndexAny(sel, ".#[:"); i >= 0 {
		tag = sel[:i]
	}
	if tag != "" && n.Data != strings.ToLower(tag) {
		return false
	}
	rest := strings.TrimPrefix(sel, tag)
	for rest != "" {
		switch rest[0] {
		case '#':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".#[")
			id := rest
			if end >= 0 {
				id = rest[:end]
				rest = rest[end:]
			} else {
				rest = ""
			}
			if attr(n, "id") != id {
				return false
			}
		case '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".#[")
			cl := rest
			if end >= 0 {
				cl = rest[:end]
				rest = rest[end:]
			} else {
				rest = ""
			}
			if !hasClass(n, cl) {
				return false
			}
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return false
			}
			expr := strings.TrimSpace(rest[1:end])
			rest = rest[end+1:]
			if i := strings.IndexByte(expr, '='); i >= 0 {
				name := strings.TrimSpace(expr[:i])
				want := strings.Trim(strings.TrimSpace(expr[i+1:]), "\"'")
				if attr(n, name) != want {
					return false
				}
			} else if !hasAttr(n, strings.TrimSpace(expr)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func firstElementChild(n *xhtml.Node) *xhtml.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			return c
		}
	}
	return nil
}

func lastElementChild(n *xhtml.Node) *xhtml.Node {
	for c := n.LastChild; c != nil; c = c.PrevSibling {
		if c.Type == xhtml.ElementNode {
			return c
		}
	}
	return nil
}

func nextElementSibling(n *xhtml.Node) *xhtml.Node {
	for c := n.NextSibling; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			return c
		}
	}
	return nil
}

func prevElementSibling(n *xhtml.Node) *xhtml.Node {
	for c := n.PrevSibling; c != nil; c = c.PrevSibling {
		if c.Type == xhtml.ElementNode {
			return c
		}
	}
	return nil
}

func allChildren(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
	}
	return out
}

func elementChildren(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

func textContentOf(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(m *xhtml.Node) {
		if m.Type == xhtml.ElementNode && (m.Data == "script" || m.Data == "style") {
			return
		}
		if m.Type == xhtml.TextNode {
			b.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	return b.String()
}

func setTextContent(n *xhtml.Node, s string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		c = next
	}
	if s != "" {
		n.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: s})
	}
}

func innerHTMLOf(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			continue
		}
	}
	return b.String()
}

func outerHTMLOf(n *xhtml.Node) string {
	var b strings.Builder
	if err := xhtml.Render(&b, n); err != nil {
		return ""
	}
	return b.String()
}

func setAttr(n *xhtml.Node, name, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: name, Val: val})
}

func removeAttr(n *xhtml.Node, name string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return
		}
	}
}

func toggleAttr(n *xhtml.Node, name string, on bool) {
	if on {
		setAttr(n, name, "")
	} else {
		removeAttr(n, name)
	}
}

// --- Inline style support ---------------------------------------------------

func inlineStyle(n *xhtml.Node) map[string]string {
	out := map[string]string{}
	raw := attr(n, "style")
	for _, part := range strings.Split(raw, ";") {
		i := strings.IndexByte(part, ':')
		if i <= 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(part[:i]))
		v := strings.TrimSpace(part[i+1:])
		if k != "" {
			out[k] = v
		}
	}
	return out
}

func styleValue(n *xhtml.Node, prop string) string {
	return inlineStyle(n)[strings.ToLower(prop)]
}

func setStyleValue(n *xhtml.Node, prop, val string) {
	props := inlineStyle(n)
	prop = strings.ToLower(strings.TrimSpace(prop))
	if prop == "" {
		return
	}
	if strings.TrimSpace(val) == "" {
		delete(props, prop)
	} else {
		props[prop] = strings.TrimSpace(val)
	}
	var keys []string
	for k := range props {
		keys = append(keys, k)
	}
	// Keep a stable order so diffs are readable.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+": "+props[k])
	}
	if len(parts) == 0 {
		removeAttr(n, "style")
	} else {
		setAttr(n, "style", strings.Join(parts, "; "))
	}
}

func (js *pageJS) styleObject(n *xhtml.Node) goja.Value {
	if o, ok := js.styles[n]; ok {
		return o
	}
	o := js.vm.NewObject()
	js.styles[n] = o
	define := func(jsName, cssName string) {
		o.DefineAccessorProperty(jsName,
			js.vm.ToValue(func(goja.FunctionCall) goja.Value {
				return js.vm.ToValue(styleValue(n, cssName))
			}),
			js.vm.ToValue(func(call goja.FunctionCall) goja.Value {
				setStyleValue(n, cssName, call.Argument(0).String())
				js.markDirty()
				return goja.Undefined()
			}), goja.FLAG_TRUE, goja.FLAG_TRUE)
	}
	for jsName, cssName := range map[string]string{
		"display": "display", "visibility": "visibility", "color": "color",
		"background": "background", "backgroundColor": "background-color",
		"fontSize": "font-size", "fontWeight": "font-weight",
		"fontStyle": "font-style", "textAlign": "text-align",
		"textDecoration": "text-decoration", "width": "width", "height": "height",
		"left": "left", "top": "top", "margin": "margin", "padding": "padding",
	} {
		define(jsName, cssName)
	}
	o.DefineAccessorProperty("cssText",
		js.vm.ToValue(func(goja.FunctionCall) goja.Value {
			return js.vm.ToValue(attr(n, "style"))
		}),
		js.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			setAttr(n, "style", call.Argument(0).String())
			js.markDirty()
			return goja.Undefined()
		}), goja.FLAG_TRUE, goja.FLAG_TRUE)
	o.Set("setProperty", func(call goja.FunctionCall) goja.Value {
		setStyleValue(n, call.Argument(0).String(), call.Argument(1).String())
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("removeProperty", func(call goja.FunctionCall) goja.Value {
		setStyleValue(n, call.Argument(0).String(), "")
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("getPropertyValue", func(call goja.FunctionCall) goja.Value {
		return js.vm.ToValue(styleValue(n, call.Argument(0).String()))
	})
	return o
}

func (js *pageJS) classListObject(n *xhtml.Node) *goja.Object {
	o := js.vm.NewObject()
	o.Set("add", func(call goja.FunctionCall) goja.Value {
		for _, name := range call.Arguments {
			cl := name.String()
			if cl != "" && !hasClass(n, cl) {
				setAttr(n, "class", strings.TrimSpace(attr(n, "class")+" "+cl))
			}
		}
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("remove", func(call goja.FunctionCall) goja.Value {
		for _, name := range call.Arguments {
			cl := name.String()
			var keep []string
			for _, f := range strings.Fields(attr(n, "class")) {
				if f != cl {
					keep = append(keep, f)
				}
			}
			setAttr(n, "class", strings.Join(keep, " "))
		}
		js.markDirty()
		return goja.Undefined()
	})
	o.Set("toggle", func(call goja.FunctionCall) goja.Value {
		cl := call.Argument(0).String()
		if hasClass(n, cl) {
			var keep []string
			for _, f := range strings.Fields(attr(n, "class")) {
				if f != cl {
					keep = append(keep, f)
				}
			}
			setAttr(n, "class", strings.Join(keep, " "))
			return js.vm.ToValue(false)
		}
		setAttr(n, "class", strings.TrimSpace(attr(n, "class")+" "+cl))
		js.markDirty()
		return js.vm.ToValue(true)
	})
	o.Set("contains", func(call goja.FunctionCall) goja.Value {
		return js.vm.ToValue(hasClass(n, call.Argument(0).String()))
	})
	return o
}

// --- Style -> layout helpers used by the HTML walker ------------------------

func styleHidden(n *xhtml.Node) bool {
	if n == nil || n.Type != xhtml.ElementNode {
		return false
	}
	if hasAttr(n, "hidden") {
		return true
	}
	st := inlineStyle(n)
	return strings.EqualFold(st["display"], "none") || strings.EqualFold(st["visibility"], "hidden")
}

func styleColorOf(n *xhtml.Node) (uint32, bool) {
	if col, ok := cssColor(attr(n, "color")); ok {
		return col, true
	}
	return cssColor(styleValue(n, "color"))
}

func cssColor(s string) (uint32, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	named := map[string]uint32{
		"black": 0x000000, "white": 0xFFFFFF, "red": 0xC00000, "green": 0x008000,
		"blue": 0x0000C0, "navy": 0x000080, "maroon": 0x800000, "purple": 0x800080,
		"gray": 0x808080, "grey": 0x808080, "silver": 0xC0C0C0, "olive": 0x808000,
		"teal": 0x008080, "orange": 0xFFA000, "yellow": 0xE0C000, "aqua": 0x00C0C0,
		"fuchsia": 0xC000C0, "lime": 0x00C000,
	}
	if c, ok := named[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			r, err1 := strconv.ParseUint(h[0:1]+h[0:1], 16, 8)
			g, err2 := strconv.ParseUint(h[1:2]+h[1:2], 16, 8)
			b, err3 := strconv.ParseUint(h[2:3]+h[2:3], 16, 8)
			if err1 == nil && err2 == nil && err3 == nil {
				return uint32(r)<<16 | uint32(g)<<8 | uint32(b), true
			}
		}
		if len(h) == 6 {
			if v, err := strconv.ParseUint(h, 16, 32); err == nil {
				return uint32(v), true
			}
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) == 3 {
			var vals [3]int
			ok := true
			for i, p := range parts {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || n < 0 || n > 255 {
					ok = false
					break
				}
				vals[i] = n
			}
			if ok {
				return uint32(vals[0])<<16 | uint32(vals[1])<<8 | uint32(vals[2]), true
			}
		}
	}
	return 0, false
}

func styleScaleOf(n *xhtml.Node) int {
	fs := strings.ToLower(strings.TrimSpace(styleValue(n, "font-size")))
	if fs == "" {
		return 0
	}
	num := fs
	if i := strings.IndexAny(num, "pxem%;"); i > 0 {
		num = num[:i]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return 0
	}
	if strings.Contains(fs, "em") {
		v *= 16
	}
	if v >= 28 {
		return 2
	}
	if v >= 18 {
		return 1
	}
	return 1
}

func styleBold(n *xhtml.Node) bool {
	v := strings.ToLower(strings.TrimSpace(styleValue(n, "font-weight")))
	return v == "bold" || v == "bolder" || v == "700" || v == "800" || v == "900"
}
