package desktop

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"win95linux/internal/ui"
)

func controlValueOf(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	if hasAttr(n, "value") {
		return attr(n, "value")
	}
	if n.Data == "textarea" {
		return strings.Trim(textContentOf(n), "\r\n")
	}
	return ""
}

func setControlValueOf(n *xhtml.Node, v string) {
	if n == nil {
		return
	}
	setAttr(n, "value", v)
	if n.Data == "textarea" {
		setTextContent(n, v)
	}
}

func (a *BrowserApp) jsEvent(n *xhtml.Node, typ string) bool {
	if a.js == nil || n == nil {
		return false
	}
	return a.js.dispatchDOMEvent(n, typ, 0, 0)
}

func (a *BrowserApp) focusControlAt(n *xhtml.Node, caret int) {
	if n == nil {
		return
	}
	if a.focusNode != n {
		if a.focusNode != nil {
			a.jsEvent(a.focusNode, "change")
			a.jsEvent(a.focusNode, "blur")
		}
		a.focusNode = n
		a.jsEvent(n, "focus")
	}
	val := []rune(controlValueOf(n))
	if caret < 0 {
		caret = 0
	}
	if caret > len(val) {
		caret = len(val)
	}
	a.focusCaret = caret
	a.focusSelAll = false
	a.d.Invalidate()
}

func (a *BrowserApp) focusControl(n *xhtml.Node) {
	if n == nil {
		return
	}
	if a.focusNode != n {
		if a.focusNode != nil {
			a.jsEvent(a.focusNode, "change")
			a.jsEvent(a.focusNode, "blur")
		}
		a.focusNode = n
		a.jsEvent(n, "focus")
	}
	a.focusCaret = utf8.RuneCountInString(controlValueOf(n))
	a.focusSelAll = false
	a.d.Invalidate()
}

func (a *BrowserApp) blurControl() {
	if a.focusNode == nil {
		return
	}
	n := a.focusNode
	a.focusNode = nil
	a.focusSelAll = false
	a.jsEvent(n, "change")
	a.jsEvent(n, "blur")
	a.d.Invalidate()
}

func (a *BrowserApp) inputChanged() {
	if a.focusNode == nil {
		return
	}
	a.jsEvent(a.focusNode, "input")
	a.d.Invalidate()
}

func (a *BrowserApp) controlDown(hit *linkHit, ev MouseEvent, d *Desktop) {
	if hit == nil || hit.ctrl == nil {
		return
	}
	ctrl := hit.ctrl
	if ctrl.disabled {
		return
	}
	switch ctrl.tag {
	case "input", "textarea":
		caret := 0
		prefix := []rune(controlValueOf(ctrl.node))
		f := ui.DefaultFont()
		rel := ev.X - (hit.rect.X + 5)
		for caret < len(prefix) && f.Width(string(prefix[:caret+1])) <= rel {
			caret++
		}
		a.focusControlAt(ctrl.node, caret)
	case "checkbox":
		on := hasAttr(ctrl.node, "checked")
		toggleAttr(ctrl.node, "checked", !on)
		ctrl.checked = !on
		a.jsEvent(ctrl.node, "click")
		a.jsEvent(ctrl.node, "change")
		d.Invalidate()
	case "radio":
		name := attr(ctrl.node, "name")
		if name != "" {
			var clear func(*xhtml.Node)
			clear = func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && n.Data == "input" &&
					strings.EqualFold(attr(n, "type"), "radio") && attr(n, "name") == name {
					removeAttr(n, "checked")
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					clear(c)
				}
			}
			clear(a.dom)
		}
		setAttr(ctrl.node, "checked", "checked")
		ctrl.checked = true
		a.jsEvent(ctrl.node, "click")
		a.jsEvent(ctrl.node, "change")
		d.Invalidate()
	case "select":
		a.cycleSelect(ctrl, d)
	case "range":
		a.setRangeFromClick(ctrl, ev, hit.rect)
	case "button":
		a.pressCtrl = ctrl
		d.Invalidate()
	case "img":
		a.pressCtrl = ctrl
		a.pressHref = hit.href
		a.pressNode = hit.node
		a.pressDown = hit.download
	}
}

func (a *BrowserApp) controlUp(hit *linkHit, ev MouseEvent, d *Desktop) bool {
	if hit == nil || hit.ctrl == nil {
		return false
	}
	ctrl := hit.ctrl
	if ctrl.tag == "button" {
		pressed := a.pressCtrl != nil && a.pressCtrl.node == ctrl.node
		a.pressCtrl = nil
		if pressed {
			prevented := a.jsEvent(ctrl.node, "click")
			if !prevented {
				typ := ctrl.inputType
				if typ == "" {
					typ = "submit"
				}
				switch typ {
				case "reset":
					a.resetForm(nearestForm(ctrl.node))
				case "submit":
					a.submitForm(nearestForm(ctrl.node))
				}
			}
			d.Invalidate()
		}
		return true
	}
	if ctrl.tag == "img" {
		if a.pressCtrl == ctrl && hit.node == a.pressNode {
			prevented := a.jsEvent(ctrl.node, "click")
			if !prevented && hit.href != "" {
				a.openLink(hit.href, hit.download || a.pressDown)
			}
		}
		a.pressCtrl = nil
		a.pressHref = ""
		a.pressNode = nil
		return true
	}
	return true
}

func (a *BrowserApp) cycleSelect(ctrl *bControl, d *Desktop) {
	if ctrl.node == nil {
		return
	}
	var opts []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			if c.Data == "option" {
				opts = append(opts, c)
			} else if c.Data == "optgroup" {
				walk(c)
			}
		}
	}
	walk(ctrl.node)
	if len(opts) == 0 {
		return
	}
	cur := -1
	for i, o := range opts {
		if hasAttr(o, "selected") {
			cur = i
		}
	}
	if cur < 0 {
		// A select without selected="" shows its first option; start cycling
		// from the option currently displayed instead of re-picking it.
		for i, o := range opts {
			t := strings.TrimSpace(textContentOf(o))
			v := attr(o, "value")
			if v == "" {
				v = t
			}
			if t == ctrl.label || v == ctrl.value {
				cur = i
				break
			}
		}
	}
	next := (cur + 1) % len(opts)
	for i, o := range opts {
		if i == next {
			setAttr(o, "selected", "selected")
		} else {
			removeAttr(o, "selected")
		}
	}
	label := strings.TrimSpace(textContentOf(opts[next]))
	value := attr(opts[next], "value")
	if value == "" {
		value = label
	}
	setAttr(ctrl.node, "value", value)
	ctrl.label = label
	ctrl.value = value
	a.jsEvent(ctrl.node, "input")
	a.jsEvent(ctrl.node, "change")
	d.Invalidate()
}

func (a *BrowserApp) setRangeFromClick(ctrl *bControl, ev MouseEvent, rect ui.Rect) {
	if rect.W <= 12 {
		return
	}
	pos := (ev.X - rect.X) * 100 / (rect.W - 12)
	if pos < 0 {
		pos = 0
	}
	if pos > 100 {
		pos = 100
	}
	minV, maxV := 0, 100
	if ctrl.node != nil {
		if v, err := strconv.Atoi(attr(ctrl.node, "min")); err == nil {
			minV = v
		}
		if v, err := strconv.Atoi(attr(ctrl.node, "max")); err == nil && v > minV {
			maxV = v
		}
	}
	v := minV + (maxV-minV)*pos/100
	setControlValueOf(ctrl.node, strconv.Itoa(v))
	ctrl.value = strconv.Itoa(v)
	a.jsEvent(ctrl.node, "input")
	a.jsEvent(ctrl.node, "change")
	a.d.Invalidate()
}

func nearestForm(n *xhtml.Node) *xhtml.Node {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == xhtml.ElementNode && cur.Data == "form" {
			return cur
		}
	}
	return nil
}

func (a *BrowserApp) formValues(form *xhtml.Node) url.Values {
	vals := url.Values{}
	if form == nil {
		return vals
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			switch c.Data {
			case "input":
				typ := strings.ToLower(attr(c, "type"))
				name := attr(c, "name")
				if name == "" {
					continue
				}
				switch typ {
				case "submit", "reset", "button", "image", "file", "hidden":
					if typ == "hidden" {
						vals.Add(name, attr(c, "value"))
					}
					continue
				case "checkbox", "radio":
					if hasAttr(c, "checked") {
						v := attr(c, "value")
						if v == "" {
							v = "on"
						}
						vals.Add(name, v)
					}
				default:
					vals.Add(name, controlValueOf(c))
				}
			case "textarea", "select":
				name := attr(c, "name")
				if name != "" {
					vals.Add(name, controlValueOf(c))
				}
			}
			walk(c)
		}
	}
	walk(form)
	return vals
}

func (a *BrowserApp) submitForm(form *xhtml.Node) {
	if form == nil {
		return
	}
	if a.js != nil && a.js.dispatchDOMEvent(form, "submit", 0, 0) {
		return
	}
	action := strings.TrimSpace(attr(form, "action"))
	target := a.curURL
	if action != "" {
		target = resolveURL(a.curURL, action)
	}
	method := strings.ToLower(strings.TrimSpace(attr(form, "method")))
	vals := a.formValues(form)
	q := vals.Encode()
	if method == "post" {
		a.status = "表单使用了 POST，内置浏览器暂以 GET 发送"
	}
	if q != "" {
		if strings.Contains(target, "?") {
			target += "&" + q
		} else {
			target += "?" + q
		}
	}
	a.Navigate(target)
}

func (a *BrowserApp) resetForm(form *xhtml.Node) {
	if form == nil {
		return
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			switch c.Data {
			case "input":
				typ := strings.ToLower(attr(c, "type"))
				if typ == "checkbox" || typ == "radio" {
					removeAttr(c, "checked")
				} else if typ != "submit" && typ != "button" {
					setAttr(c, "value", "")
				}
			case "textarea":
				setControlValueOf(c, "")
			}
			walk(c)
		}
	}
	walk(form)
	a.d.Invalidate()
}

func (a *BrowserApp) focusNextControl(dir int, d *Desktop) bool {
	var controls []*xhtml.Node
	if a.dom == nil {
		return false
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "input":
				typ := strings.ToLower(attr(n, "type"))
				if typ != "hidden" && typ != "submit" && typ != "button" && typ != "reset" && !hasAttr(n, "disabled") {
					controls = append(controls, n)
				}
			case "textarea", "select":
				if !hasAttr(n, "disabled") {
					controls = append(controls, n)
				}
			case "button":
				if !hasAttr(n, "disabled") {
					controls = append(controls, n)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(a.dom)
	if len(controls) == 0 {
		return false
	}
	idx := -1
	for i, c := range controls {
		if c == a.focusNode {
			idx = i
			break
		}
	}
	next := idx + dir
	if next < 0 {
		next = len(controls) - 1
	}
	if next >= len(controls) {
		next = 0
	}
	a.focusControl(controls[next])
	return true
}

func (a *BrowserApp) keyToControl(ev KeyEvent, d *Desktop) bool {
	n := a.focusNode
	if n == nil {
		return false
	}
	if hasAttr(n, "disabled") {
		if ev.Name == "Esc" {
			a.blurControl()
			return true
		}
		return true
	}
	readonly := hasAttr(n, "readonly")
	val := []rune(controlValueOf(n))
	if a.focusCaret > len(val) {
		a.focusCaret = len(val)
	}
	if a.focusCaret < 0 {
		a.focusCaret = 0
	}
	setVal := func(v []rune) {
		setControlValueOf(n, string(v))
		a.inputChanged()
	}
	delSelection := func() bool {
		if a.focusSelAll {
			a.focusSelAll = false
			setVal(nil)
			a.focusCaret = 0
			return true
		}
		return false
	}
	switch ev.Name {
	case "Esc":
		a.blurControl()
		return true
	case "Tab":
		a.blurControl()
		a.focusNextControl(1, d)
		return true
	case "Enter":
		if n.Data == "textarea" && !ev.Ctrl {
			if !readonly {
				if !delSelection() {
					val = append(val[:a.focusCaret], append([]rune{'\n'}, val[a.focusCaret:]...)...)
					setVal(val)
					a.focusCaret++
				}
			}
			return true
		}
		a.jsEvent(n, "change")
		if f := nearestForm(n); f != nil {
			a.submitForm(f)
		} else {
			a.blurControl()
		}
		return true
	case "Backspace":
		if readonly {
			return true
		}
		if !delSelection() && a.focusCaret > 0 {
			val = append(val[:a.focusCaret-1], val[a.focusCaret:]...)
			a.focusCaret--
			setVal(val)
		}
		return true
	case "Delete":
		if readonly {
			return true
		}
		if !delSelection() && a.focusCaret < len(val) {
			val = append(val[:a.focusCaret], val[a.focusCaret+1:]...)
			setVal(val)
		}
		return true
	case "Left":
		a.focusSelAll = false
		if a.focusCaret > 0 {
			a.focusCaret--
		}
		d.Invalidate()
		return true
	case "Right":
		a.focusSelAll = false
		if a.focusCaret < len(val) {
			a.focusCaret++
		}
		d.Invalidate()
		return true
	case "Home":
		a.focusSelAll = false
		a.focusCaret = 0
		d.Invalidate()
		return true
	case "End":
		a.focusSelAll = false
		a.focusCaret = len(val)
		d.Invalidate()
		return true
	}
	if ev.Ctrl && (ev.Rune == 'a' || ev.Rune == 'A' || ev.Name == "A") {
		a.focusSelAll = true
		a.focusCaret = len(val)
		d.Invalidate()
		return true
	}
	if ev.Rune != 0 && !ev.Ctrl && !ev.Alt {
		if readonly {
			return true
		}
		text := []rune{ev.Rune}
		if a.focusSelAll {
			a.focusSelAll = false
			val = nil
			a.focusCaret = 0
		}
		if maxLen, err := strconv.Atoi(attr(n, "maxlength")); err == nil && maxLen > 0 && len(val)+len(text) > maxLen {
			return true
		}
		val = append(val[:a.focusCaret], append(text, val[a.focusCaret:]...)...)
		a.focusCaret += len(text)
		setVal(val)
		return true
	}
	return false
}
