package desktop

import (
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"win95linux/internal/ui"
)

type bStyle struct {
	scale     int
	bold      bool
	href      string
	download  bool
	underline bool
	col       uint32
	hasCol    bool
}

type bSpan struct {
	x, w int
	text string
	st   bStyle
}

type bLine struct {
	y, h  int
	spans []bSpan
	rule  bool
}

type bDoc struct {
	title  string
	lines  []bLine
	height int
}

type hSeg struct {
	text string
	st   bStyle
}

type hBlock struct {
	segs          []hSeg
	pre           bool
	indent        int
	bullet        string
	center        bool
	rule          bool
	before, after int
}

type hCtx struct {
	href      string
	download  bool
	bold      bool
	code      bool
	underline bool
	pre       bool
	indent    int
	center    bool
	scale     int
}

type htmlWalker struct {
	blocks []hBlock
	cur    hBlock
}

func attr(n *xhtml.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *xhtml.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

func findTitle(n *xhtml.Node) string {
	if n.Type == xhtml.ElementNode && n.Data == "title" {
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.TextNode {
				b.WriteString(c.Data)
			}
		}
		return strings.TrimSpace(b.String())
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

func findBody(n *xhtml.Node) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

func (w *htmlWalker) add(text string, ctx hCtx) {
	if text == "" {
		return
	}
	st := bStyle{
		scale: 1, bold: ctx.bold, href: ctx.href, download: ctx.download,
		underline: ctx.underline,
	}
	if ctx.scale > 1 {
		st.scale = ctx.scale
	}
	if ctx.code {
		st.col = 0x800000
		st.hasCol = true
	}
	w.cur.segs = append(w.cur.segs, hSeg{text: text, st: st})
}

func (w *htmlWalker) flush(after int) {
	nonEmpty := false
	for _, s := range w.cur.segs {
		if strings.TrimSpace(s.text) != "" {
			nonEmpty = true
			break
		}
	}
	if len(w.cur.segs) > 0 && (nonEmpty || w.cur.pre) {
		w.cur.after = after
		w.blocks = append(w.blocks, w.cur)
	} else if w.cur.rule {
		w.cur.after = after
		w.blocks = append(w.blocks, w.cur)
	}
	w.cur = hBlock{}
}

func (w *htmlWalker) begin(before int, ctx hCtx) {
	w.flush(0)
	w.cur.before = before
	w.cur.indent = ctx.indent
	w.cur.pre = ctx.pre
	w.cur.center = ctx.center
}

func (w *htmlWalker) walkChildren(n *xhtml.Node, ctx hCtx) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c, ctx)
	}
}

func (w *htmlWalker) walk(n *xhtml.Node, ctx hCtx) {
	switch n.Type {
	case xhtml.TextNode:
		w.add(n.Data, ctx)
		return
	case xhtml.ElementNode:
	default:
		return
	}
	tag := n.Data
	switch tag {
	case "script", "style", "noscript", "template", "svg", "link", "meta":
		return
	case "head", "title":
		return
	case "br":
		w.add("\u2028", ctx)
		return
	case "hr":
		w.flush(4)
		w.blocks = append(w.blocks, hBlock{rule: true, before: 8, after: 8})
		return
	case "img":
		alt := strings.TrimSpace(attr(n, "alt"))
		if alt == "" {
			alt = "图片"
		}
		w.add("[图片: "+alt+"]", ctx)
		return
	case "p":
		w.begin(6, ctx)
		w.walkChildren(n, ctx)
		w.flush(8)
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		scale := 1
		if tag == "h1" || tag == "h2" {
			scale = 2
		}
		c2 := ctx
		c2.bold = true
		c2.scale = scale
		if tag == "h1" {
			c2.center = true
		}
		w.begin(12, c2)
		w.walkChildren(n, c2)
		w.flush(8)
		return
	case "div", "section", "article", "header", "footer", "main", "nav", "form", "fieldset":
		w.begin(2, ctx)
		w.walkChildren(n, ctx)
		w.flush(4)
		return
	case "blockquote":
		c2 := ctx
		c2.indent += 24
		w.begin(8, c2)
		w.walkChildren(n, c2)
		w.flush(8)
		return
	case "pre":
		c2 := ctx
		c2.pre = true
		w.begin(6, c2)
		w.walkChildren(n, c2)
		w.flush(8)
		return
	case "center":
		c2 := ctx
		c2.center = true
		w.begin(2, c2)
		w.walkChildren(n, c2)
		w.flush(2)
		return
	case "ul", "ol":
		w.walkList(n, ctx, tag == "ol")
		return
	case "li":
		w.walkListItem(n, ctx, "• ")
		return
	case "table":
		w.begin(6, ctx)
		w.walkChildren(n, ctx)
		w.flush(6)
		return
	case "tr":
		w.begin(2, ctx)
		w.walkChildren(n, ctx)
		w.flush(2)
		return
	case "td":
		w.add("  ", ctx)
		w.walkChildren(n, ctx)
		w.add("  |  ", ctx)
		return
	case "th":
		c2 := ctx
		c2.bold = true
		w.add("  ", ctx)
		w.walkChildren(n, c2)
		w.add("  |  ", ctx)
		return
	case "a":
		c2 := ctx
		if href := attr(n, "href"); href != "" {
			c2.href = href
			c2.download = hasAttr(n, "download")
			c2.underline = true
		}
		w.walkChildren(n, c2)
		return
	case "b", "strong":
		c2 := ctx
		c2.bold = true
		w.walkChildren(n, c2)
		return
	case "u", "ins":
		c2 := ctx
		c2.underline = true
		w.walkChildren(n, c2)
		return
	case "code", "kbd", "samp", "tt":
		c2 := ctx
		c2.code = true
		w.walkChildren(n, c2)
		return
	case "i", "em":
		c2 := ctx
		c2.bold = false
		w.walkChildren(n, c2)
		return
	case "select":
		w.add("[下拉列表]", ctx)
		return
	case "textarea":
		w.add("[文本区域]", ctx)
		return
	case "input":
		t := attr(n, "type")
		if t == "submit" || t == "button" || t == "reset" {
			v := attr(n, "value")
			if v == "" {
				v = "按钮"
			}
			w.add("["+v+"]", ctx)
		} else if t == "checkbox" || t == "radio" {
			w.add("[ ]", ctx)
		} else {
			w.add("[输入框]", ctx)
		}
		return
	case "button":
		w.add("[", ctx)
		w.walkChildren(n, ctx)
		w.add("]", ctx)
		return
	case "iframe":
		w.add("[内嵌框架]", ctx)
		return
	case "video", "audio":
		w.add("[媒体内容]", ctx)
		return
	}
	w.walkChildren(n, ctx)
}

func (w *htmlWalker) walkList(n *xhtml.Node, ctx hCtx, ordered bool) {
	w.flush(2)
	base := ctx.indent + 16
	idx := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		if c.Type == xhtml.ElementNode && c.Data == "li" {
			idx++
			bullet := "• "
			if ordered {
				bullet = fmt.Sprintf("%d. ", idx)
			}
			w.walkListItem(c, ctx, bullet)
			continue
		}
		c2 := ctx
		c2.indent = base
		w.walk(c, c2)
	}
	w.flush(4)
}

func (w *htmlWalker) walkListItem(li *xhtml.Node, ctx hCtx, bullet string) {
	w.flush(0)
	w.cur.before = 2
	w.cur.indent = ctx.indent + 16
	w.cur.bullet = bullet
	w.cur.pre = ctx.pre
	w.cur.center = ctx.center
	c2 := ctx
	c2.indent = ctx.indent + 16
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c, c2)
	}
	w.flush(2)
}

func layoutHTML(raw string, width int) *bDoc {
	root, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return layoutText(raw, width)
	}
	w := &htmlWalker{}
	body := findBody(root)
	if body == nil {
		body = root
	}
	w.walkChildren(body, hCtx{indent: 0})
	w.flush(0)
	doc := layoutBlocks(w.blocks, width)
	doc.title = findTitle(root)
	return doc
}

func layoutText(raw string, width int) *bDoc {
	w := &htmlWalker{}
	w.cur.pre = true
	w.cur.segs = []hSeg{{text: raw, st: bStyle{scale: 1}}}
	w.flush(0)
	return layoutBlocks(w.blocks, width)
}

func layoutBlocks(blocks []hBlock, width int) *bDoc {
	if width < 80 {
		width = 80
	}
	d := &bDoc{}
	y := 8
	for _, b := range blocks {
		y += b.before
		if b.rule {
			d.lines = append(d.lines, bLine{y: y, h: 1, rule: true})
			y += 9 + b.after
			continue
		}
		if len(b.segs) == 0 {
			y += b.after
			continue
		}
		bl := &blockLayout{font: ui.DefaultFont(), width: width, doc: d, y: y, block: b}
		bl.run()
		y = bl.y + b.after
	}
	d.height = y + 20
	if len(d.lines) == 0 {
		d.lines = append(d.lines, bLine{y: 8, h: 18})
		d.height = 40
	}
	return d
}

type blockLayout struct {
	font         *ui.Font
	width        int
	doc          *bDoc
	y            int
	block        hBlock
	line         bLine
	x            int
	startX       int
	pendingSpace bool
}

func (l *blockLayout) run() {
	l.startX = l.block.indent
	l.newLine()
	if l.block.bullet != "" {
		bw := l.font.Width(l.block.bullet)
		l.line.spans = append(l.line.spans, bSpan{x: l.startX, w: bw, text: l.block.bullet, st: bStyle{scale: 1}})
		l.x += bw
		l.startX = l.x
	}
	for _, seg := range l.block.segs {
		l.addText(seg.text, seg.st)
	}
	l.flushLine()
	l.line = bLine{}
}

func (l *blockLayout) newLine() {
	l.line = bLine{y: l.y, h: 18}
	l.x = l.startX
	l.pendingSpace = false
}

func (l *blockLayout) styleOf(st bStyle) bStyle {
	if st.scale < 1 {
		st.scale = 1
	}
	return st
}

func (l *blockLayout) addSpan(text string, st bStyle, w int) {
	st = l.styleOf(st)
	h := 16*st.scale + 4
	if h > l.line.h {
		l.line.h = h
	}
	l.line.spans = append(l.line.spans, bSpan{x: l.x, w: w, text: text, st: st})
	l.x += w
}

func (l *blockLayout) space(st bStyle) {
	if l.block.pre {
		l.addSpan(" ", st, l.font.RuneWidth(' ')*l.styleOf(st).scale)
		return
	}
	if !l.pendingSpace {
		return
	}
	l.pendingSpace = false
	if l.x <= l.startX {
		return
	}
	l.addSpan(" ", st, l.font.RuneWidth(' ')*l.styleOf(st).scale)
}

func (l *blockLayout) hardBreak() {
	if len(l.line.spans) > 0 {
		l.flushLine()
	} else {
		l.line.h = 18
		l.doc.lines = append(l.doc.lines, l.line)
		l.y += 18
		l.line = bLine{y: l.y, h: 18}
		l.x = l.startX
	}
	l.newLine()
}

func (l *blockLayout) addText(s string, st bStyle) {
	st = l.styleOf(st)
	if l.block.pre {
		for _, r := range s {
			if r == '\n' {
				l.hardBreak()
				continue
			}
			l.addChar(r, st)
		}
		return
	}
	var word strings.Builder
	wordW := 0
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		if wordW <= l.width-l.startX && l.x+wordW <= l.width {
			l.addSpan(word.String(), st, wordW)
		} else {
			for _, r := range word.String() {
				l.addChar(r, st)
			}
		}
		word.Reset()
		wordW = 0
	}
	for _, r := range s {
		switch {
		case r == '\u2028':
			flushWord()
			l.hardBreak()
		case unicode.IsSpace(r):
			flushWord()
			l.pendingSpace = true
		case isWideRune(r):
			flushWord()
			l.space(st)
			l.addChar(r, st)
		default:
			if l.pendingSpace && word.Len() == 0 {
				flushWord()
				l.space(st)
			}
			word.WriteRune(r)
			wordW += l.font.RuneWidth(r) * st.scale
		}
	}
	flushWord()
}

func (l *blockLayout) addChar(r rune, st bStyle) {
	w := l.font.RuneWidth(r) * st.scale
	if w <= 0 {
		return
	}
	if l.x+w > l.width && l.x > l.startX {
		l.flushLine()
		l.newLine()
	}
	l.addSpan(string(r), st, w)
}

func (l *blockLayout) flushLine() {
	if len(l.line.spans) == 0 {
		return
	}
	if l.block.center {
		maxRight := 0
		for _, sp := range l.line.spans {
			if sp.x+sp.w > maxRight {
				maxRight = sp.x + sp.w
			}
		}
		if maxRight < l.width {
			delta := (l.width - maxRight) / 2
			for i := range l.line.spans {
				l.line.spans[i].x += delta
			}
		}
	}
	l.doc.lines = append(l.doc.lines, l.line)
	l.y += l.line.h
	l.line = bLine{y: l.y, h: 18}
	l.x = l.startX
	l.pendingSpace = false
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF:
		return true
	}
	return false
}

type pageKind int

const (
	pageHTML pageKind = iota
	pageText
)

type histEntry struct {
	url   string
	raw   string
	title string
	kind  pageKind
}

type downloadInfo struct {
	path string
	size int64
}

type fetchResult struct {
	entry    histEntry
	download *downloadInfo
	err      error
	finalURL string
}

type linkHit struct {
	rect     ui.Rect
	href     string
	download bool
}

type BrowserApp struct {
	Base
	d   *Desktop
	win *Window

	mu      sync.Mutex
	curURL  string
	addr    string
	title   string
	raw     string
	kind    pageKind
	doc     *bDoc
	history []histEntry
	histIdx int
	visited map[string]bool
	loading bool
	err     string
	status  string
	gen     int

	addrEditing bool
	addrSelAll  bool
	addrCaret   int
	hoverHref   string
	pressHref   string
	pressDown   bool

	scroll     VScroll
	layoutW    int
	lastClient ui.Rect

	btnBack, btnFwd, btnStop, btnRefresh, btnHome, btnGo ui.Rect
	addrR, contentR, statusR, innerR, scrollR            ui.Rect
}

func NewBrowserApp(d *Desktop, start string) *BrowserApp {
	a := &BrowserApp{d: d, visited: make(map[string]bool), histIdx: -1}
	if start == "" {
		start = "about:home"
	}
	a.Navigate(start)
	return a
}

func (a *BrowserApp) Title() string {
	if a.title != "" {
		return a.title + " - 网络浏览器"
	}
	return "网络浏览器"
}

func (a *BrowserApp) Icon() ui.IconKind       { return ui.IcoBrowser }
func (a *BrowserApp) DefaultSize() (int, int) { return 900, 640 }
func (a *BrowserApp) MinSize() (int, int)     { return 540, 380 }
func (a *BrowserApp) Resizable() bool         { return true }

func (a *BrowserApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "打开地址(L)", Accel: "Ctrl+L", Enabled: true, Action: func() { a.focusAddr() }},
			{Label: "下载文件夹(D)", Enabled: true, Action: func() { d.openFiles(downloadDir()) }},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "后退(B)", Enabled: a.histIdx > 0, Action: func() { a.goBack() }},
			{Label: "前进(F)", Enabled: a.histIdx >= 0 && a.histIdx < len(a.history)-1, Action: func() { a.goForward() }},
			{Separator: true},
			{Label: "刷新(R)", Accel: "F5", Enabled: true, Action: func() { a.reload() }},
			{Label: "主页(H)", Enabled: true, Action: func() { a.Navigate("about:home") }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "浏览器帮助(A)", Enabled: true, Action: func() { a.Navigate("about:help") }},
			{Label: "关于(A)", Enabled: true, Action: func() {
				d.MessageBox("关于网络浏览器", "Windows 95 Linux 网络浏览器\n版本 1.0\n\n支持 HTML 页面、超链接、本地文件、HTTP/HTTPS 访问与文件下载。\n下载的文件保存在 "+downloadDir()+"。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func downloadDir() string {
	for _, dir := range []string{"/root/下载", filepath.Join(os.TempDir(), "win95-下载")} {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return os.TempDir()
}

func (a *BrowserApp) focusAddr() {
	a.addrEditing = true
	a.addrSelAll = true
	a.addrCaret = utf8.RuneCountInString(a.addr)
	a.d.Invalidate()
}

func (a *BrowserApp) Navigate(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "about:home"
	}
	raw = normalizeURL(raw)
	a.gen++
	gen := a.gen
	a.loading = true
	a.err = ""
	a.status = "正在打开 " + raw + " ..."
	a.addr = raw
	a.addrEditing = false
	a.hoverHref = ""
	a.d.Invalidate()
	go func() {
		res := fetchPage(raw, false)
		a.d.Post(func() {
			if gen != a.gen {
				return
			}
			a.applyResult(res)
		})
	}()
}

func (a *BrowserApp) openLink(href string, download bool) {
	if href == "" {
		return
	}
	target := resolveURL(a.curURL, href)
	a.NavigateWith(target, download)
}

func (a *BrowserApp) NavigateWith(raw string, forceDownload bool) {
	raw = normalizeURL(raw)
	a.gen++
	gen := a.gen
	a.loading = true
	a.err = ""
	a.status = "正在打开 " + raw + " ..."
	a.addr = raw
	a.addrEditing = false
	a.hoverHref = ""
	a.d.Invalidate()
	go func() {
		res := fetchPage(raw, forceDownload)
		a.d.Post(func() {
			if gen != a.gen {
				return
			}
			a.applyResult(res)
		})
	}()
}

func (a *BrowserApp) applyResult(res fetchResult) {
	a.loading = false
	if res.download != nil {
		a.addr = a.curURL
		a.status = fmt.Sprintf("已下载 %s（%d 字节）", filepath.Base(res.download.path), res.download.size)
		path := res.download.path
		a.d.MessageBox("下载完成",
			"文件已保存到：\n"+path+"\n\n大小："+formatBytes(res.download.size),
			IconInfo, []string{"打开文件夹", "确定"}, func(i int) {
				if i == 0 {
					a.d.openFiles(filepath.Dir(path))
				}
			})
		a.d.Invalidate()
		return
	}
	if res.err != nil {
		a.err = res.err.Error()
		a.status = "无法打开页面"
		errHTML := errorPageHTML(res.err.Error(), res.finalURL)
		if res.finalURL != "" {
			a.curURL = res.finalURL
		}
		a.raw = errHTML
		a.kind = pageHTML
		a.doc = layoutHTML(errHTML, a.contentWidth())
		a.title = "无法打开页面"
		a.addr = a.curURL
		a.InvalidateLayout()
		return
	}
	e := res.entry
	if res.finalURL != "" {
		e.url = res.finalURL
	}
	a.curURL = e.url
	a.raw = e.raw
	a.kind = e.kind
	a.title = e.title
	a.addr = e.url
	a.err = ""
	a.status = "完成"
	a.visited[stripFragment(e.url)] = true
	if a.histIdx >= 0 && a.histIdx < len(a.history) && a.history[a.histIdx].url == e.url && a.history[a.histIdx].raw == e.raw {
		// Reload replaces the current entry.
	} else {
		if a.histIdx >= 0 && a.histIdx < len(a.history)-1 {
			a.history = a.history[:a.histIdx+1]
		}
		a.history = append(a.history, e)
		if len(a.history) > 40 {
			a.history = a.history[len(a.history)-40:]
		}
		a.histIdx = len(a.history) - 1
	}
	a.rebuild()
}

func (a *BrowserApp) InvalidateLayout() {
	a.layoutW = 0
	a.scroll.Pos = 0
	a.d.Invalidate()
}

func (a *BrowserApp) contentWidth() int {
	if a.lastClient.W > 0 {
		w := a.lastClient.W - 36
		if w < 80 {
			w = 80
		}
		return w
	}
	return 640
}

func (a *BrowserApp) rebuild() {
	if a.curURL == "" {
		a.curURL = a.addr
	}
	w := a.contentWidth()
	if w < 80 {
		w = 80
	}
	if a.kind == pageText {
		a.doc = layoutText(a.raw, w)
		if a.title == "" {
			a.title = a.curURL
		}
	} else {
		a.doc = layoutHTML(a.raw, w)
		if a.doc.title != "" {
			a.title = a.doc.title
		}
	}
	a.scroll.Pos = 0
	a.layoutW = w
	a.d.Invalidate()
}

func (a *BrowserApp) goBack() {
	if a.histIdx <= 0 {
		return
	}
	a.histIdx--
	a.showHistory()
}

func (a *BrowserApp) goForward() {
	if a.histIdx < 0 || a.histIdx >= len(a.history)-1 {
		return
	}
	a.histIdx++
	a.showHistory()
}

func (a *BrowserApp) showHistory() {
	if a.histIdx < 0 || a.histIdx >= len(a.history) {
		return
	}
	e := a.history[a.histIdx]
	a.gen++
	a.loading = false
	a.curURL = e.url
	a.raw = e.raw
	a.kind = e.kind
	a.title = e.title
	a.addr = e.url
	a.addrEditing = false
	a.err = ""
	a.status = "完成"
	a.rebuild()
	a.d.Invalidate()
}

func (a *BrowserApp) reload() {
	if a.curURL == "" {
		return
	}
	if a.histIdx >= 0 && a.histIdx < len(a.history) {
		a.history = a.history[:a.histIdx]
	}
	a.Navigate(a.curURL)
}

func (a *BrowserApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	a.lastClient = client
	a.layoutRects(client)
	c.FillRect(client, ui.ColFace)
	f := ui.DefaultFont()

	a.paintToolbar(c, f, client)

	inner := a.innerR
	c.FillRect(a.contentR, ui.ColField)
	c.ThinSunken(a.contentR)
	c.FillRect(inner, ui.ColWhite)

	a.mu.Lock()
	doc := a.doc
	loading := a.loading
	hover := a.hoverHref
	status := a.status
	errText := a.err
	a.mu.Unlock()

	if doc == nil || (loading && len(doc.lines) <= 1) {
		msg := "正在加载页面，请稍候..."
		if errText != "" {
			msg = "无法打开页面：" + errText
		}
		f.TextClip(c, inner.X+20, inner.Y+28, inner.W-40, msg, ui.ColShadow)
		if loading {
			ui.ProgressBar(c, ui.Rect{inner.X + 20, inner.Y + 56, inner.W - 40, 14}, -1)
		}
	} else {
		a.paintDoc(c, f, doc, inner)
	}

	a.paintScrollbar(c, client)

	a.statusR = ui.Rect{client.X, client.Bottom() - 22, client.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColWhite)
	c.ThinSunken(ui.Rect{client.X + 2, a.statusR.Y + 3, client.W - 4, 17})
	st := status
	if hover != "" {
		st = "链接: " + resolveURL(a.curURL, hover)
	}
	if errText != "" {
		st = "错误: " + errText
	}
	f.TextClip(c, client.X+7, a.statusR.Y+5, client.W-110, st, ui.ColText)
	right := "完成"
	if loading {
		right = "正在加载…"
	}
	f.TextRight(c, client.Right()-8, a.statusR.Y+5, right, ui.ColText)
}

func (a *BrowserApp) layoutRects(client ui.Rect) {
	const tbH = 36
	cy := client.Y + 4
	x := client.X + 4
	a.btnBack = ui.Rect{x, cy, 30, 26}
	x += 32
	a.btnFwd = ui.Rect{x, cy, 30, 26}
	x += 32
	a.btnStop = ui.Rect{x, cy, 50, 26}
	x += 52
	a.btnRefresh = ui.Rect{x, cy, 50, 26}
	x += 52
	a.btnHome = ui.Rect{x, cy, 50, 26}
	x += 60
	labelW := ui.DefaultFont().Width("地址:")
	addrLabelX := x
	_ = addrLabelX
	x += labelW + 4
	a.btnGo = ui.Rect{client.Right() - 60, cy, 56, 26}
	a.addrR = ui.Rect{x, cy, a.btnGo.X - 6 - x, 26}
	if a.addrR.W < 80 {
		a.addrR.W = 80
	}
	a.contentR = ui.Rect{client.X, client.Y + tbH, client.W, client.H - tbH - 22}
	a.innerR = ui.Rect{a.contentR.X + 2, a.contentR.Y + 2, a.contentR.W - 20, a.contentR.H - 4}
	a.scrollR = ui.Rect{a.contentR.Right() - 18, a.contentR.Y + 2, 16, a.contentR.H - 4}
	if a.innerR.W < 40 {
		a.innerR.W = 40
	}
	if a.innerR.H < 20 {
		a.innerR.H = 20
	}
	w := a.innerR.W - 16
	if w < 80 {
		w = 80
	}
	if w != a.layoutW && a.doc != nil && a.raw != "" {
		a.rebuild()
	}
}

func (a *BrowserApp) paintToolbar(c *ui.Canvas, f *ui.Font, client ui.Rect) {
	ui.Button(c, a.btnBack, "", false, true)
	ui.Arrow(c, a.btnBack.X+11, a.btnBack.Y+9, "left", ui.ColDark)
	ui.Button(c, a.btnFwd, "", false, true)
	ui.Arrow(c, a.btnFwd.X+14, a.btnFwd.Y+9, "right", ui.ColDark)
	ui.Button(c, a.btnStop, "", false, true)
	f.Text(c, a.btnStop.X+12, a.btnStop.Y+5, "停止", ui.ColText)
	ui.Button(c, a.btnRefresh, "", false, true)
	f.Text(c, a.btnRefresh.X+12, a.btnRefresh.Y+5, "刷新", ui.ColText)
	ui.Button(c, a.btnHome, "", false, true)
	f.Text(c, a.btnHome.X+12, a.btnHome.Y+5, "主页", ui.ColText)

	label := "地址:"
	f.Text(c, a.btnHome.Right()+8, a.btnHome.Y+5, label, ui.ColText)
	c.FillRect(a.addrR, ui.ColField)
	c.Sunken(a.addrR)

	text := a.addr
	if !a.addrEditing {
		f.TextClip(c, a.addrR.X+4, a.addrR.Y+4, a.addrR.W-8, text, ui.ColText)
	} else {
		runes := []rune(text)
		caret := a.addrCaret
		if caret > len(runes) {
			caret = len(runes)
		}
		avail := a.addrR.W - 8
		start := 0
		total := f.Width(string(runes))
		if total > avail {
			start = caret
			for start > 0 {
				if f.Width(string(runes[start:caret])) <= avail-8 {
					break
				}
				start--
			}
		}
		shown := string(runes[start:])
		f.TextClip(c, a.addrR.X+4, a.addrR.Y+4, avail, shown, ui.ColText)
		if a.addrSelAll && len(runes) > 0 {
			c.FillRect(ui.Rect{a.addrR.X + 4, a.addrR.Y + 3, f.Width(shown), 19}, ui.ColNavy)
			f.Text(c, a.addrR.X+4, a.addrR.Y+4, shown, ui.ColWhite)
		} else if int(time.Now().UnixMilli()/500)%2 == 0 {
			cx := a.addrR.X + 4 + f.Width(string(runes[start:caret]))
			c.VLine(cx, a.addrR.Y+4, 17, ui.ColDark)
		}
	}
	ui.Button(c, a.btnGo, "", false, true)
	f.Text(c, a.btnGo.X+13, a.btnGo.Y+5, "转到", ui.ColText)
}

func (a *BrowserApp) paintDoc(c *ui.Canvas, f *ui.Font, doc *bDoc, inner ui.Rect) {
	a.mu.Lock()
	pos := a.scroll.Pos
	base := a.curURL
	visited := make(map[string]bool, len(a.visited))
	for k, v := range a.visited {
		visited[k] = v
	}
	a.mu.Unlock()
	for _, line := range doc.lines {
		y := inner.Y + line.y - pos
		if y+line.h <= inner.Y || y >= inner.Bottom() {
			continue
		}
		if y < inner.Y || y+line.h > inner.Bottom() {
			continue
		}
		if line.rule {
			c.HLine(inner.X+8, y+4, inner.W-16, ui.ColShadow)
			c.HLine(inner.X+8, y+5, inner.W-16, ui.ColWhite)
			continue
		}
		for _, sp := range line.spans {
			if sp.text == "" {
				continue
			}
			col := ui.ColText
			if sp.st.hasCol {
				col = sp.st.col
			}
			if sp.st.href != "" {
				href := stripFragment(resolveURL(base, sp.st.href))
				if visited[href] {
					col = 0x800080
				} else {
					col = ui.ColNavy
				}
			}
			x := inner.X + 8 + sp.x
			scale := sp.st.scale
			if scale < 1 {
				scale = 1
			}
			if sp.st.bold {
				f.Scaled(c, x, y+2, sp.text, col, scale)
				f.Scaled(c, x+1, y+2, sp.text, col, scale)
			} else {
				f.Scaled(c, x, y+2, sp.text, col, scale)
			}
			if sp.st.href != "" || sp.st.underline {
				c.HLine(x, y+2+16*scale, sp.w, col)
			}
		}
	}
}

func (a *BrowserApp) paintScrollbar(c *ui.Canvas, client ui.Rect) {
	doc := a.doc
	total := 0
	if doc != nil {
		total = doc.height
	}
	a.scroll.Total = total
	a.scroll.Visible = a.innerR.H
	a.scroll.Clamp()
	a.scroll.Draw(c, a.scrollR)
}

func (a *BrowserApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layoutRects(client)
	if ev.Kind == MouseWheel {
		before := a.scroll.Pos
		a.scroll.ScrollBy(-ev.Wheel * 48)
		if a.scroll.Pos != before {
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseDown {
		switch {
		case a.btnBack.Contains(ev.X, ev.Y):
			a.goBack()
			return
		case a.btnFwd.Contains(ev.X, ev.Y):
			a.goForward()
			return
		case a.btnStop.Contains(ev.X, ev.Y):
			a.gen++
			a.loading = false
			a.status = "已停止"
			d.Invalidate()
			return
		case a.btnRefresh.Contains(ev.X, ev.Y):
			a.reload()
			return
		case a.btnHome.Contains(ev.X, ev.Y):
			a.Navigate("about:home")
			return
		case a.btnGo.Contains(ev.X, ev.Y):
			a.Navigate(a.addr)
			return
		case a.addrR.Contains(ev.X, ev.Y):
			a.addrEditing = true
			a.addrSelAll = false
			runes := []rune(a.addr)
			idx := 0
			for idx < len(runes) {
				if ui.DefaultFont().Width(string(runes[:idx+1])) > ev.X-a.addrR.X-4 {
					break
				}
				idx++
			}
			a.addrCaret = idx
			d.Invalidate()
			return
		}
		if a.addrEditing {
			a.addrEditing = false
			a.addr = a.curURL
			d.Invalidate()
		}
	}
	if ev.Kind == MouseDown || ev.Kind == MouseUp || ev.Kind == MouseMove {
		changed, _ := a.scroll.Mouse(ev, a.scrollR)
		if changed {
			d.Invalidate()
			return
		}
	}
	if !a.contentR.Contains(ev.X, ev.Y) {
		if ev.Kind == MouseMove && a.hoverHref != "" {
			a.hoverHref = ""
			d.SetHand(false)
			d.Invalidate()
		}
		return
	}
	hit := a.linkAt(ev.X, ev.Y)
	if ev.Kind == MouseMove {
		href := ""
		if hit != nil {
			href = hit.href
		}
		if href != a.hoverHref {
			a.hoverHref = href
			a.d.SetHand(hit != nil)
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseDown {
		a.pressHref = ""
		a.pressDown = false
		if hit != nil {
			a.pressHref = hit.href
			a.pressDown = hit.download
			a.d.SetHand(true)
		}
		return
	}
	if ev.Kind == MouseUp {
		if hit != nil && hit.href == a.pressHref && hit.href != "" {
			a.openLink(hit.href, hit.download || a.pressDown)
		}
		a.pressHref = ""
	}
}

func (a *BrowserApp) linkAt(gx, gy int) *linkHit {
	doc := a.doc
	if doc == nil {
		return nil
	}
	for _, line := range doc.lines {
		y := a.innerR.Y + line.y - a.scroll.Pos
		if gy < y || gy >= y+line.h {
			continue
		}
		for _, sp := range line.spans {
			if sp.st.href == "" {
				continue
			}
			x := a.innerR.X + 8 + sp.x
			if gx >= x && gx < x+sp.w && gx < a.innerR.Right() {
				return &linkHit{
					rect:     ui.Rect{x, y, sp.w, line.h},
					href:     sp.st.href,
					download: sp.st.download,
				}
			}
		}
	}
	return nil
}

func (a *BrowserApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.addrEditing {
		switch ev.Name {
		case "Enter":
			a.addrEditing = false
			a.Navigate(a.addr)
			return true
		case "Esc":
			a.addrEditing = false
			a.addr = a.curURL
			d.Invalidate()
			return true
		case "Backspace":
			runes := []rune(a.addr)
			if a.addrSelAll {
				a.addr = ""
				a.addrCaret = 0
				a.addrSelAll = false
			} else if a.addrCaret > 0 && a.addrCaret <= len(runes) {
				a.addr = string(append(runes[:a.addrCaret-1], runes[a.addrCaret:]...))
				a.addrCaret--
			}
			d.Invalidate()
			return true
		case "Delete":
			runes := []rune(a.addr)
			if a.addrSelAll {
				a.addr = ""
				a.addrCaret = 0
				a.addrSelAll = false
			} else if a.addrCaret < len(runes) {
				a.addr = string(append(runes[:a.addrCaret], runes[a.addrCaret+1:]...))
			}
			d.Invalidate()
			return true
		case "Left":
			if a.addrCaret > 0 {
				a.addrCaret--
			}
			a.addrSelAll = false
			d.Invalidate()
			return true
		case "Right":
			if a.addrCaret < utf8.RuneCountInString(a.addr) {
				a.addrCaret++
			}
			a.addrSelAll = false
			d.Invalidate()
			return true
		case "Home":
			a.addrCaret = 0
			a.addrSelAll = false
			d.Invalidate()
			return true
		case "End":
			a.addrCaret = utf8.RuneCountInString(a.addr)
			a.addrSelAll = false
			d.Invalidate()
			return true
		}
		if ev.Ctrl && (ev.Rune == 'a' || ev.Rune == 'A' || ev.Name == "A") {
			a.addrSelAll = true
			a.addrCaret = utf8.RuneCountInString(a.addr)
			d.Invalidate()
			return true
		}
		if ev.Rune != 0 && !ev.Ctrl && !ev.Alt {
			var runes []rune
			if a.addrSelAll {
				a.addrSelAll = false
				a.addrCaret = 0
			} else {
				runes = []rune(a.addr)
				if a.addrCaret > len(runes) {
					a.addrCaret = len(runes)
				}
			}
			if runes == nil {
				a.addr = string(ev.Rune)
				a.addrCaret = 1
			} else {
				runes = append(runes[:a.addrCaret], append([]rune{ev.Rune}, runes[a.addrCaret:]...)...)
				a.addr = string(runes)
				a.addrCaret++
			}
			d.Invalidate()
			return true
		}
		return true
	}
	switch {
	case ev.Name == "F5":
		a.reload()
	case ev.Ctrl && (ev.Rune == 'l' || ev.Rune == 'L' || ev.Name == "L"):
		a.focusAddr()
	case ev.Alt && ev.Name == "Left":
		a.goBack()
	case ev.Alt && ev.Name == "Right":
		a.goForward()
	case ev.Name == "Backspace":
		a.goBack()
	case ev.Name == "PgUp":
		a.scroll.ScrollBy(-a.innerR.H)
		d.Invalidate()
	case ev.Name == "PgDn":
		a.scroll.ScrollBy(a.innerR.H)
		d.Invalidate()
	case ev.Name == "Up":
		a.scroll.ScrollBy(-48)
		d.Invalidate()
	case ev.Name == "Down":
		a.scroll.ScrollBy(48)
		d.Invalidate()
	case ev.Name == "Home":
		a.scroll.Pos = 0
		d.Invalidate()
	case ev.Name == "End":
		a.scroll.Pos = a.scroll.Max()
		d.Invalidate()
	default:
		return false
	}
	return true
}

func (a *BrowserApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	a.lastClient = client
	a.InvalidateLayout()
}

func (a *BrowserApp) Close() {}

func normalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "about:home"
	}
	lower := strings.ToLower(s)
	for _, p := range []string{"http://", "https://", "file://", "about:", "mailto:"} {
		if strings.HasPrefix(lower, p) {
			return s
		}
	}
	if strings.HasPrefix(s, "/") {
		return "file://" + s
	}
	if strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/root"
		}
		return "file://" + filepath.Join(home, s[2:])
	}
	if strings.HasPrefix(s, "\\\\") {
		return "file://" + filepath.ToSlash(strings.TrimPrefix(s, "\\\\"))
	}
	host := s
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	if host == "localhost" || host == "127.0.0.1" || isIPv4(host) {
		return "http://" + s
	}
	return "https://" + s
}

func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func resolveURL(base, ref string) string {
	if ref == "" {
		return base
	}
	if strings.HasPrefix(ref, "#") {
		return stripFragment(base) + ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func stripFragment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}

func fetchPage(raw string, forceDownload bool) fetchResult {
	res := fetchResult{finalURL: raw}
	lower := strings.ToLower(raw)
	switch {
	case lower == "about:home":
		res.entry = histEntry{url: raw, raw: homePageHTML, title: "欢迎使用网络浏览器", kind: pageHTML}
		return res
	case lower == "about:help" || lower == "about:about":
		res.entry = histEntry{url: raw, raw: helpPageHTML, title: "浏览器帮助", kind: pageHTML}
		return res
	case strings.HasPrefix(lower, "about:"):
		res.entry = histEntry{url: raw, raw: simplePageHTML("关于 "+raw, "此页面是浏览器内置页面。请访问主页或输入其他地址。"), title: raw, kind: pageHTML}
		return res
	case strings.HasPrefix(lower, "mailto:"):
		res.err = fmt.Errorf("本浏览器不支持直接发送邮件")
		return res
	case strings.HasPrefix(lower, "file://"):
		return fetchFile(raw, forceDownload)
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		return fetchHTTP(raw, forceDownload)
	}
	res.err = fmt.Errorf("不支持的地址: %s", raw)
	return res
}

var browserHTTP = &http.Client{
	Timeout: 30 * time.Second,
}

func fetchHTTP(raw string, forceDownload bool) fetchResult {
	res := fetchResult{finalURL: raw}
	req, err := http.NewRequest("GET", raw, nil)
	if err != nil {
		res.err = fmt.Errorf("地址无效: %v", err)
		return res
	}
	req.Header.Set("User-Agent", "Win95LinuxBrowser/1.0 (ARM64)")
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.8")
	resp, err := browserHTTP.Do(req)
	if err != nil {
		res.err = fmt.Errorf("网络错误: %v", err)
		return res
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		res.finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		res.err = fmt.Errorf("服务器返回 %s\n%s", resp.Status, strings.TrimSpace(string(body)))
		return res
	}
	ct := resp.Header.Get("Content-Type")
	media, params, _ := mime.ParseMediaType(ct)
	force := forceDownload
	if resp.Header.Get("Content-Disposition") != "" {
		if _, cdParams, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
			if cdParams["filename"] != "" {
				params["filename"] = cdParams["filename"]
			}
		}
	}
	if force || (media != "" && media != "text/html" && media != "text/plain" && media != "application/xhtml+xml" && !strings.HasPrefix(media, "text/")) {
		name := params["filename"]
		if name == "" {
			name = filenameFromURL(res.finalURL)
		}
		dl, err := saveDownload(resp.Body, name)
		if err != nil {
			res.err = err
			return res
		}
		res.download = dl
		return res
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		res.err = fmt.Errorf("读取页面失败: %v", err)
		return res
	}
	isHTML := media == "text/html" || media == "application/xhtml+xml" ||
		(media == "" && strings.HasSuffix(strings.ToLower(res.finalURL), ".html"))
	if !isHTML && media == "" && looksLikeHTML(body) {
		isHTML = true
	}
	entry := histEntry{url: res.finalURL, raw: string(body), title: res.finalURL}
	if isHTML {
		entry.kind = pageHTML
	} else {
		entry.kind = pageText
	}
	res.entry = entry
	return res
}

func fetchFile(raw string, forceDownload bool) fetchResult {
	res := fetchResult{finalURL: raw}
	u, err := url.Parse(raw)
	if err != nil {
		res.err = fmt.Errorf("文件地址无效: %v", err)
		return res
	}
	if u.Host != "" && u.Host != "localhost" {
		res.err = fmt.Errorf("不支持远程文件主机: %s", u.Host)
		return res
	}
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	if decode, err := url.PathUnescape(p); err == nil {
		p = decode
	}
	if p == "" {
		res.err = fmt.Errorf("文件路径为空")
		return res
	}
	st, err := os.Stat(p)
	if err != nil {
		res.err = fmt.Errorf("无法访问 %s: %v", p, err)
		return res
	}
	if st.IsDir() {
		res.entry = histEntry{url: "file://" + p, raw: simplePageHTML("文件夹", "这是一个文件夹：<br><code>"+html.EscapeString(p)+"</code><br><br>请使用文件管理器浏览。"), title: filepath.Base(p), kind: pageHTML}
		res.finalURL = "file://" + p
		return res
	}
	ext := strings.ToLower(filepath.Ext(p))
	isHTML := ext == ".html" || ext == ".htm" || ext == ".xhtml" || ext == ".shtml"
	isText := isHTML || ext == ".txt" || ext == ".md" || ext == ".markdown" || ext == ".log" ||
		ext == ".ini" || ext == ".conf" || ext == ".cfg" || ext == ".sh" || ext == ".csv" ||
		ext == ".json" || ext == ".xml" || ext == ".yaml" || ext == ".yml" || ext == ".go" ||
		ext == ".c" || ext == ".h" || ext == ".py" || ext == ".js" || ext == ".css"
	if forceDownload || !isText {
		src, err := os.Open(p)
		if err != nil {
			res.err = fmt.Errorf("无法读取文件: %v", err)
			return res
		}
		defer src.Close()
		dl, err := saveDownload(src, filepath.Base(p))
		if err != nil {
			res.err = err
			return res
		}
		res.download = dl
		res.finalURL = "file://" + p
		return res
	}
	body, err := os.ReadFile(p)
	if err != nil {
		res.err = fmt.Errorf("无法读取文件: %v", err)
		return res
	}
	if len(body) > 8<<20 {
		res.err = fmt.Errorf("文件超过 8MB，请使用下载方式打开")
		return res
	}
	kind := pageText
	if isHTML {
		kind = pageHTML
	}
	res.entry = histEntry{url: "file://" + p, raw: string(body), title: filepath.Base(p), kind: kind}
	res.finalURL = "file://" + p
	return res
}

func saveDownload(r io.Reader, name string) (*downloadInfo, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "下载文件"
	}
	dir := downloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建下载目录: %v", err)
	}
	dest := uniquePath(dir, name)
	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("无法创建文件: %v", err)
	}
	const limit = 128 << 20
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dest)
		return nil, fmt.Errorf("下载失败: %v", err)
	}
	if n > limit {
		os.Remove(dest)
		return nil, fmt.Errorf("文件超过 128MB，已取消下载")
	}
	return &downloadInfo{path: dest, size: n}, nil
}

func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem = "下载文件"
	}
	cand := filepath.Join(dir, name)
	if _, err := os.Stat(cand); os.IsNotExist(err) {
		return cand
	}
	for i := 2; i < 10000; i++ {
		cand = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().Unix(), ext))
}

func filenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "下载文件"
	}
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		name = "下载文件"
	}
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	return name
}

func looksLikeHTML(b []byte) bool {
	s := strings.ToLower(string(b))
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "<!doctype html") || strings.HasPrefix(s, "<html") ||
		strings.Contains(s[:min(len(s), 512)], "<body")
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d 字节", n)
}

func simplePageHTML(title, body string) string {
	return "<!DOCTYPE html><html><head><title>" + html.EscapeString(title) + "</title></head><body><h2>" +
		html.EscapeString(title) + "</h2><p>" + body + "</p></body></html>"
}

func errorPageHTML(errText, target string) string {
	body := "<h2>无法打开页面</h2>" +
		"<p>浏览器无法显示此页面。</p>" +
		"<p><b>错误信息：</b><br>" + html.EscapeString(errText) + "</p>" +
		"<p><b>地址：</b><br>" + html.EscapeString(target) + "</p>" +
		"<hr><p>请检查网络连接和地址是否正确。如果使用的是安装盘环境，请先完成安装。</p>" +
		"<p><a href=\"about:home\">返回浏览器主页</a></p>"
	return simplePageHTML("无法打开页面", body)
}

const homePageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head><title>欢迎使用网络浏览器</title></head>
<body>
<h1>Windows 95 Linux 网络浏览器</h1>
<p>欢迎！这是一个内置的轻量浏览器。您可以点击蓝色链接浏览本地页面，也可以在地址栏输入网址访问互联网。</p>
<hr>
<h2>本地演示</h2>
<ul>
<li><a href="file:///usr/share/win95/第二页.html">打开本地演示页面</a></li>
<li><a href="file:///usr/share/win95/示例文件.txt" download>下载示例文本文件</a>（保存到“下载”文件夹）</li>
<li><a href="file:///root/文档/欢迎使用.txt">阅读“欢迎使用.txt”</a></li>
<li><a href="file:///usr/share/win95/示例网页.html">产品介绍页面</a></li>
</ul>
<h2>互联网</h2>
<p>在地址栏输入 <code>example.com</code> 或完整的 <code>https://</code> 网址即可访问网络（需要虚拟机具备网络连接）。</p>
<p><a href="https://example.com/">访问 example.com</a></p>
<hr>
<h2>下载说明</h2>
<p>当链接指向的文件不是网页或文本时，浏览器会自动将其下载到 <code>/root/下载</code> 文件夹，并显示提示窗口。带“下载”标记的链接无论类型都会直接保存。</p>
<p>下载完成后可以从“开始 → 文档 → 下载文件夹”中查看文件。</p>
<hr>
<p><a href="about:help">浏览器帮助</a> · <a href="about:home">主页</a></p>
<p><small>Windows 95 Linux 中文版 · ARM64 / UEFI</small></p>
</body>
</html>`

const helpPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head><title>浏览器帮助</title></head>
<body>
<h1>浏览器帮助</h1>
<h2>基本操作</h2>
<ul>
<li><b>地址栏</b>：输入网址后按 Enter，或点击“转到”。</li>
<li><b>后退 / 前进</b>：使用工具栏按钮，或按 Alt+← / Alt+→。</li>
<li><b>刷新</b>：按 F5 重新载入当前页面。</li>
<li><b>滚动</b>：使用鼠标滚轮、滚动条，或 PgUp / PgDn / Home / End。</li>
<li><b>选择地址</b>：按 Ctrl+L 快速选中地址栏内容。</li>
</ul>
<h2>支持的地址</h2>
<ul>
<li><code>https://example.com</code> — 互联网网页</li>
<li><code>file:///root/文档/欢迎使用.txt</code> — 本地文件</li>
<li><code>about:home</code> — 浏览器主页</li>
</ul>
<h2>下载</h2>
<p>图片、压缩包等非网页文件会自动下载到 <code>/root/下载</code>。如果链接带有 <code>download</code> 属性，则始终按下载处理。</p>
<h2>安全</h2>
<p>浏览器使用系统的 CA 证书验证 HTTPS 连接。在离线或证书无效时会显示错误页面，不会忽略证书错误。</p>
<hr>
<p><a href="about:home">返回主页</a></p>
</body>
</html>`
