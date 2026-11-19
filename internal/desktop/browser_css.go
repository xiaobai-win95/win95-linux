package desktop

import (
	"strings"

	xhtml "golang.org/x/net/html"
)

// The built-in browser is deliberately small, so the CSS engine only supports
// the handful of properties that affect the block layout and form controls.
type cssRule struct {
	selector string
	decls    map[string]string
}

var cssWanted = map[string]bool{
	"color": true, "background": true, "background-color": true,
	"font-size": true, "font-weight": true, "text-align": true,
	"text-decoration": true, "display": true, "visibility": true,
}

func parseCSS(text string) []cssRule {
	for {
		i := strings.Index(text, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(text[i+2:], "*/")
		if j < 0 {
			text = text[:i]
			break
		}
		text = text[:i] + " " + text[i+2+j+2:]
	}
	var rules []cssRule
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(text[open:], '}')
		if close < 0 {
			break
		}
		selector := strings.TrimSpace(text[:open])
		body := text[open+1 : open+close]
		text = text[open+close+1:]
		if selector == "" || strings.HasPrefix(selector, "@") {
			continue
		}
		decls := make(map[string]string)
		for _, decl := range strings.Split(body, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if !ok {
				continue
			}
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.TrimSpace(v)
			if !cssWanted[k] || v == "" || strings.Contains(v, "!important") {
				continue
			}
			decls[k] = v
		}
		if len(decls) == 0 {
			continue
		}
		for _, sel := range strings.Split(selector, ",") {
			sel = strings.TrimSpace(sel)
			if sel == "" || strings.ContainsAny(sel, ":[>+~") {
				continue
			}
			rules = append(rules, cssRule{selector: sel, decls: decls})
		}
	}
	return rules
}

func styleText(root *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "style" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == xhtml.TextNode {
					b.WriteString(c.Data)
				}
			}
			return
		}
		if n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "noscript") {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return b.String()
}

func matchSimpleSelector(n *xhtml.Node, sel string) bool {
	if n == nil || n.Type != xhtml.ElementNode {
		return false
	}
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return false
	}
	if sel == "*" {
		return true
	}
	tag := sel
	id := ""
	class := ""
	if i := strings.IndexByte(sel, '#'); i >= 0 {
		id = sel[i+1:]
		tag = sel[:i]
	}
	if i := strings.IndexByte(tag, '.'); i >= 0 {
		class = tag[i+1:]
		tag = tag[:i]
	}
	if i := strings.IndexByte(class, '.'); i >= 0 {
		class = class[:i]
	}
	if tag != "" && !strings.EqualFold(n.Data, tag) {
		return false
	}
	if id != "" && attr(n, "id") != id {
		return false
	}
	if class != "" && !hasClass(n, class) {
		return false
	}
	return true
}

func matchCSSSelector(n *xhtml.Node, sel string) bool {
	parts := strings.Fields(sel)
	if len(parts) == 0 {
		return false
	}
	if !matchSimpleSelector(n, parts[len(parts)-1]) {
		return false
	}
	idx := len(parts) - 2
	for cur := n.Parent; cur != nil && idx >= 0; cur = cur.Parent {
		if matchSimpleSelector(cur, parts[idx]) {
			idx--
		}
	}
	return idx < 0
}

// applyDocumentStyles folds <style> rules into style attributes so the rest of
// the layout code can keep reading inline styles only. Inline declarations win
// over stylesheet rules, as in a real browser.
func applyDocumentStyles(root *xhtml.Node) {
	if root == nil {
		return
	}
	text := styleText(root)
	if strings.TrimSpace(text) == "" {
		return
	}
	rules := parseCSS(text)
	if len(rules) == 0 {
		return
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			var merged map[string]string
			for _, rule := range rules {
				if !matchCSSSelector(n, rule.selector) {
					continue
				}
				if merged == nil {
					merged = make(map[string]string)
				}
				for k, v := range rule.decls {
					merged[k] = v
				}
			}
			if len(merged) > 0 {
				inline := inlineStyle(n)
				var b strings.Builder
				b.WriteString(attr(n, "style"))
				for k, v := range merged {
					if _, ok := inline[k]; ok {
						continue
					}
					if b.Len() > 0 && !strings.HasSuffix(strings.TrimSpace(b.String()), ";") {
						b.WriteByte(';')
					}
					b.WriteString(k)
					b.WriteByte(':')
					b.WriteString(v)
					b.WriteByte(';')
				}
				setAttr(n, "style", b.String())
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

func styleBackgroundOf(n *xhtml.Node) (uint32, bool) {
	if n == nil {
		return 0, false
	}
	if col, ok := cssColor(attr(n, "bgcolor")); ok {
		return col, true
	}
	if col, ok := cssColor(styleValue(n, "background-color")); ok {
		return col, true
	}
	if bg := strings.TrimSpace(styleValue(n, "background")); bg != "" {
		for _, part := range strings.Fields(bg) {
			if col, ok := cssColor(part); ok {
				return col, true
			}
		}
	}
	return 0, false
}
