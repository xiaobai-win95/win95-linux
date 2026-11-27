package markdown

import (
	"reflect"
	"strings"
	"testing"

	"win95linux/internal/ui"
)

func colorCount(c *ui.Canvas, col uint32) int {
	count := 0
	for _, pixel := range c.Pix {
		if pixel == col {
			count++
		}
	}
	return count
}

func longestColorRun(c *ui.Canvas, col uint32) int {
	longest := 0
	for y := 0; y < c.H; y++ {
		run := 0
		for x := 0; x < c.W; x++ {
			if c.Pix[y*c.Stride+x] == col {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
	}
	return longest
}

func TestRenderFeaturesAndViewportClipping(t *testing.T) {
	doc := mustParse(t, "# Heading\n\n**bold** plain *italic* `code` [link](https://example.com) $x^2$ $\\frac{a}{b}$\n\n- one\n  - nested\n\n| A | B |\n| --- | ---: |\n| x | 2 |")
	const sentinel = uint32(0x123456)
	c := ui.NewCanvas(440, 520)
	for i := range c.Pix {
		c.Pix[i] = sentinel
	}
	r := ui.Rect{X: 10, Y: 10, W: 420, H: 500}
	result := Render(c, r, doc, Options{})
	if result.Viewport != r || result.ContentHeight <= 0 {
		t.Errorf("unexpected result: %#v", result)
	}
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if !r.Contains(x, y) && c.Pix[y*c.Stride+x] != sentinel {
				t.Fatalf("pixel outside viewport changed at %d,%d", x, y)
			}
		}
	}
	for name, col := range map[string]uint32{
		"text": ui.ColText, "link": ui.ColBlue, "inline code": ui.ColNavy,
		"table header": ui.ColFace, "borders": ui.ColShadow,
	} {
		if colorCount(c, col) == 0 {
			t.Errorf("no %s pixels were rendered", name)
		}
	}
}

func TestInlineStylesDoNotLeakToSiblings(t *testing.T) {
	doc := mustParse(t, "**b** p *i* q `c` r [l](https://e) n")
	styles := make(map[string]textStyle)
	for _, atom := range buildInlineAtoms(doc.Blocks[0].Inlines, textStyle{scale: 1}) {
		if atom.kind == atomText {
			styles[atom.text] = atom.style
		}
	}
	for text, style := range styles {
		if style.bold != (text == "b") || style.italic != (text == "i") || style.code != (text == "c") || style.link != (text == "l") {
			t.Errorf("style leaked for %q: %#v", text, style)
		}
	}
}

func TestCodeCopyButtonsAndScrolling(t *testing.T) {
	code := "fmt.Println(\"hello\")\n// second line"
	doc := mustParse(t, "```go\n"+code+"\n```\n\n"+strings.Repeat("A paragraph of text.\n\n", 20))
	c := ui.NewCanvas(300, 180)
	r := ui.Rect{X: 10, Y: 10, W: 280, H: 160}
	copies := 0
	var copied string
	opts := Options{CopyCode: func(text string) {
		copies++
		copied = text
	}}
	result := Render(c, r, doc, opts)
	if copies != 0 {
		t.Fatal("Render invoked CopyCode without a click")
	}
	if len(result.CopyButtons) != 1 || result.MaxScroll <= 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	button := result.CopyButtons[0]
	if button.Code != code {
		t.Errorf("copy code = %q", button.Code)
	}
	if !result.Activate(button.Rect.X+button.Rect.W/2, button.Rect.Y+button.Rect.H/2) {
		t.Error("copy button did not activate")
	}
	if copies != 1 || copied != code {
		t.Errorf("copy callback got %q, %d calls", copied, copies)
	}
	if result.Activate(r.X-1, r.Y-1) {
		t.Error("click outside copy button activated")
	}

	scrolled := Render(c, r, doc, Options{CopyCode: opts.CopyCode, ScrollOffset: 10})
	if scrolled.ContentRect.Y != r.Y-10 || len(scrolled.CopyButtons) != 1 {
		t.Fatalf("unexpected scrolled result: %#v", scrolled)
	}
	if scrolled.CopyButtons[0].Rect.Y != button.Rect.Y-10 {
		t.Errorf("button did not move with scroll: %#v -> %#v", button.Rect, scrolled.CopyButtons[0].Rect)
	}
	end := Render(c, r, doc, Options{CopyCode: opts.CopyCode, ScrollOffset: 100000})
	if end.ContentRect.Y != r.Y-end.MaxScroll || len(end.CopyButtons) != 0 {
		t.Errorf("scroll was not clamped: %#v", end)
	}

	disabled := Render(c, r, Document{Blocks: []Block{{Kind: BlockCode, Code: "x"}}}, Options{})
	if len(disabled.CopyButtons) != 1 || disabled.Activate(disabled.CopyButtons[0].Rect.X+1, disabled.CopyButtons[0].Rect.Y+1) {
		t.Errorf("nil copy callback should disable activation: %#v", disabled)
	}
}

func TestRenderClippingAndTinyRects(t *testing.T) {
	const sentinel = uint32(0x234567)
	doc := mustParse(t, "# A heading\n\nSome **text** and a table.\n\n| A | B |\n| --- | --- |\n| 1 | 2 |")
	c := ui.NewCanvas(80, 80)
	for i := range c.Pix {
		c.Pix[i] = sentinel
	}
	r := ui.Rect{X: 20, Y: 15, W: 40, H: 45}
	Render(c, r, doc, Options{})
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if !r.Contains(x, y) && c.Pix[y*c.Stride+x] != sentinel {
				t.Fatalf("pixel outside r changed at %d,%d", x, y)
			}
		}
	}

	c = ui.NewCanvas(60, 60)
	for i := range c.Pix {
		c.Pix[i] = sentinel
	}
	partial := ui.Rect{X: -5, Y: 5, W: 30, H: 30}
	result := Render(c, partial, doc, Options{})
	if result.Viewport != (ui.Rect{X: 0, Y: 5, W: 25, H: 30}) {
		t.Errorf("partial viewport = %#v", result.Viewport)
	}
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if !partial.Contains(x, y) && c.Pix[y*c.Stride+x] != sentinel {
				t.Fatalf("pixel outside partial rect changed at %d,%d", x, y)
			}
		}
	}
	Render(c, ui.Rect{X: 1, Y: 1, W: 1, H: 1}, doc, Options{})
	Render(c, ui.Rect{X: 1, Y: 1, W: 0, H: 10}, doc, Options{})
}

func TestWrappingAndContentHeight(t *testing.T) {
	doc := mustParse(t, strings.Repeat("word ", 100))
	wide := layoutInline(doc.Blocks[0].Inlines, 400, textStyle{scale: 1})
	narrow := layoutInline(doc.Blocks[0].Inlines, 80, textStyle{scale: 1})
	if len(narrow) <= len(wide) {
		t.Errorf("narrow layout has %d lines, wide has %d", len(narrow), len(wide))
	}
	wideResult := Render(ui.NewCanvas(440, 500), ui.Rect{W: 420, H: 480}, doc, Options{})
	narrowResult := Render(ui.NewCanvas(120, 500), ui.Rect{W: 100, H: 480}, doc, Options{})
	if narrowResult.ContentHeight <= wideResult.ContentHeight {
		t.Errorf("narrow height %d <= wide height %d", narrowResult.ContentHeight, wideResult.ContentHeight)
	}
}

func TestMathRenderedWithCanvasPrimitives(t *testing.T) {
	doc := mustParse(t, `$x^2$ and $\frac{a}{b}$`)
	c := ui.NewCanvas(300, 120)
	Render(c, ui.Rect{X: 10, Y: 10, W: 280, H: 100}, doc, Options{})
	if colorCount(c, ui.ColText) == 0 {
		t.Fatal("math text was not drawn")
	}
	if run := longestColorRun(c, ui.ColText); run < 20 {
		t.Errorf("fraction bar was not drawn as a horizontal primitive (longest run %d)", run)
	}
}

func TestTableLayoutUsesAvailableWidth(t *testing.T) {
	doc := mustParse(t, "| A | B | C |\n| :--- | :---: | ---: |\n| one | two | three |")
	block := doc.Blocks[0]
	layout := layoutTable(block, 198)
	total := 0
	for _, width := range layout.widths {
		total += width
	}
	if total != 198 || len(layout.rows) != 2 || layout.height < 58 {
		t.Errorf("unexpected table layout: %#v", layout)
	}
	align := make([]TableAlignment, len(layout.widths))
	for i := range align {
		align[i] = layout.rows[0][i].align
	}
	if !reflect.DeepEqual(align, []TableAlignment{AlignLeft, AlignCenter, AlignRight}) {
		t.Errorf("cell alignments = %v", align)
	}
}

func TestEmptyNilAndInvalidRenderInputs(t *testing.T) {
	if got := Render(nil, ui.Rect{W: 10, H: 10}, Document{}, Options{}); !reflect.DeepEqual(got, RenderResult{}) {
		t.Errorf("nil canvas result = %#v", got)
	}
	c := ui.NewCanvas(100, 100)
	before := append([]uint32(nil), c.Pix...)
	if got := Render(c, ui.Rect{W: 0, H: 10}, Document{}, Options{}); !reflect.DeepEqual(got, RenderResult{}) {
		t.Errorf("invalid rect result = %#v", got)
	}
	if !reflect.DeepEqual(before, c.Pix) {
		t.Error("invalid render changed the canvas")
	}
	r := ui.Rect{X: 5, Y: 5, W: 90, H: 90}
	firstCanvas, secondCanvas := ui.NewCanvas(100, 100), ui.NewCanvas(100, 100)
	first := Render(firstCanvas, r, Document{}, Options{})
	second := Render(secondCanvas, r, Document{}, Options{ScrollOffset: -100})
	if first.ContentHeight != 2*documentPadding || first.ContentRect != (ui.Rect{X: 5, Y: 5, W: 90, H: 16}) {
		t.Errorf("empty result = %#v", first)
	}
	if !reflect.DeepEqual(first.ContentRect, second.ContentRect) || !reflect.DeepEqual(firstCanvas.Pix, secondCanvas.Pix) {
		t.Error("negative scroll offset was not treated as zero")
	}
}
