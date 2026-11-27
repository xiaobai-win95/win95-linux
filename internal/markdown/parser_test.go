package markdown

import (
	"reflect"
	"strings"
	"testing"

	"win95linux/internal/ui"
)

func mustParse(t *testing.T, text string) Document {
	t.Helper()
	doc, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func plainText(nodes []Inline) string {
	var out strings.Builder
	for _, node := range nodes {
		if node.Kind == InlineText || node.Kind == InlineCode || node.Kind == InlineMath {
			out.WriteString(node.Text)
		} else {
			out.WriteString(plainText(node.Children))
		}
	}
	return out.String()
}

func countInline(nodes []Inline, kind InlineKind) int {
	count := 0
	for _, node := range nodes {
		if node.Kind == kind {
			count++
		}
		count += countInline(node.Children, kind)
	}
	return count
}

func TestParseDocumentBlocksAndInlines(t *testing.T) {
	input := "# Title\r\n" +
		"Paragraph with **bold**, *italic*, `code`, [link](https://example.com), $x^2$, and \\(\\frac{a}{b}\\).\r\n" +
		"```go\r\nfmt.Println(\"hi\")\r\n```\r\n" +
		"| Name | Value |\r\n| :--- | ---: |\r\n| a | 2 |\r\n" +
		"- first\r\n  - nested\r\n3. third\r\n"
	doc := mustParse(t, input)
	if len(doc.Blocks) != 6 {
		t.Fatalf("got %d blocks, want 6: %#v", len(doc.Blocks), doc.Blocks)
	}
	heading := doc.Blocks[0]
	if heading.Kind != BlockHeading || heading.Level != 1 || plainText(heading.Inlines) != "Title" {
		t.Errorf("unexpected heading: %#v", heading)
	}
	paragraph := doc.Blocks[1]
	if paragraph.Kind != BlockParagraph {
		t.Fatalf("paragraph kind = %v", paragraph.Kind)
	}
	for _, kind := range []InlineKind{InlineBold, InlineItalic, InlineCode, InlineLink, InlineMath} {
		if countInline(paragraph.Inlines, kind) == 0 {
			t.Errorf("paragraph is missing inline kind %v", kind)
		}
	}
	if countInline(paragraph.Inlines, InlineMath) != 2 {
		t.Errorf("math count = %d, want 2", countInline(paragraph.Inlines, InlineMath))
	}
	var link *Inline
	for i := range paragraph.Inlines {
		if paragraph.Inlines[i].Kind == InlineLink {
			link = &paragraph.Inlines[i]
		}
	}
	if link == nil || link.URL != "https://example.com" || plainText(link.Children) != "link" {
		t.Errorf("unexpected link: %#v", link)
	}
	code := doc.Blocks[2]
	if code.Kind != BlockCode || code.Language != "go" || code.Code != `fmt.Println("hi")` {
		t.Errorf("unexpected code block: %#v", code)
	}
	table := doc.Blocks[3]
	if table.Kind != BlockTable || len(table.Header) != 2 || len(table.Rows) != 1 {
		t.Fatalf("unexpected table: %#v", table)
	}
	if !reflect.DeepEqual(table.Align, []TableAlignment{AlignLeft, AlignRight}) {
		t.Errorf("table alignments = %v", table.Align)
	}
	if plainText(table.Header[0]) != "Name" || plainText(table.Rows[0][1]) != "2" {
		t.Errorf("unexpected table contents: %#v", table)
	}
	list := doc.Blocks[4]
	if list.Kind != BlockList || list.Ordered || len(list.Items) != 1 {
		t.Fatalf("unexpected unordered list: %#v", list)
	}
	if len(list.Items[0].Blocks) != 2 || list.Items[0].Blocks[1].Kind != BlockList {
		t.Errorf("nested list was not parsed: %#v", list.Items[0])
	}
	ordered := doc.Blocks[5]
	if !ordered.Ordered || ordered.Start != 3 || len(ordered.Items) != 1 {
		t.Errorf("unexpected ordered list: %#v", ordered)
	}
}

func TestInlineSyntaxAndEscapes(t *testing.T) {
	input := "escaped \\*star\\* and __bold__ + ***both***; foo_bar_baz; ``a ` tick``; [a [b]](https://e/x_(y) \"title\"); \\(x^2\\)"
	doc := mustParse(t, input)
	nodes := doc.Blocks[0].Inlines
	text := plainText(nodes)
	for _, want := range []string{"escaped *star*", "bold", "both", "foo_bar_baz", "a ` tick", "a [b]", "x^2"} {
		if !strings.Contains(text, want) {
			t.Errorf("inline text %q does not contain %q", text, want)
		}
	}
	if countInline(nodes, InlineBold) != 2 || countInline(nodes, InlineItalic) != 1 {
		t.Errorf("unexpected emphasis counts: %#v", nodes)
	}
	if countInline(nodes, InlineCode) != 1 || countInline(nodes, InlineMath) != 1 {
		t.Errorf("unexpected code/math counts: %#v", nodes)
	}
	for _, node := range nodes {
		if node.Kind == InlineLink && node.URL != "https://e/x_(y)" {
			t.Errorf("link URL = %q", node.URL)
		}
	}
}

func TestInlineHardAndSoftBreaks(t *testing.T) {
	doc := mustParse(t, "soft\nbreak  \nhard\\\nslash")
	nodes := doc.Blocks[0].Inlines
	if countInline(nodes, InlineBreak) != 2 {
		t.Fatalf("break count = %d, want 2: %#v", countInline(nodes, InlineBreak), nodes)
	}
	if got := plainText(nodes); got != "soft breakhardslash" {
		t.Errorf("text = %q", got)
	}
}

func TestFencesAreLenient(t *testing.T) {
	doc := mustParse(t, "~~~~go extra\nline\n~~~\nstill\n~~~~")
	if len(doc.Blocks) != 1 || doc.Blocks[0].Code != "line\n~~~\nstill" || doc.Blocks[0].Language != "go" {
		t.Fatalf("unexpected tilde fence: %#v", doc.Blocks)
	}
	doc = mustParse(t, "```\na\nb")
	if doc.Blocks[0].Code != "a\nb" {
		t.Errorf("unclosed fence code = %q", doc.Blocks[0].Code)
	}
	doc = mustParse(t, "a | b\n-- | --")
	if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != BlockParagraph {
		t.Errorf("malformed table should remain a paragraph: %#v", doc.Blocks)
	}
}

func TestTableRowSplitting(t *testing.T) {
	cells, ok := splitTableRow("| a \\| b | `c|d` |")
	if !ok || len(cells) != 2 {
		t.Fatalf("split = %#v, %v", cells, ok)
	}
	if cells[0] != `a \| b` || cells[1] != "`c|d`" {
		t.Errorf("cells = %#v", cells)
	}
	row, err := parseTableRow(cells)
	if err != nil {
		t.Fatal(err)
	}
	if plainText(row[0]) != "a | b" || countInline(row[1], InlineCode) != 1 {
		t.Errorf("parsed row = %#v", row)
	}
	for _, tc := range []struct {
		cell string
		want TableAlignment
	}{
		{":---", AlignLeft}, {"---:", AlignRight}, {":---:", AlignCenter}, {"---", AlignLeft},
	} {
		got, ok := parseTableAlignment(tc.cell)
		if !ok || got != tc.want {
			t.Errorf("parseTableAlignment(%q) = %v, %v", tc.cell, got, ok)
		}
	}
}

func TestListLazyContinuationAndNesting(t *testing.T) {
	doc := mustParse(t, "- first line\n  continued\n  1. nested one\n  2. nested two\n- second")
	list := doc.Blocks[0]
	if len(list.Items) != 2 || len(list.Items[0].Blocks) != 2 {
		t.Fatalf("unexpected list: %#v", list)
	}
	if got := plainText(list.Items[0].Blocks[0].Inlines); got != "first line continued" {
		t.Errorf("lazy continuation text = %q", got)
	}
	nested := list.Items[0].Blocks[1]
	if !nested.Ordered || nested.Start != 1 || len(nested.Items) != 2 {
		t.Errorf("unexpected nested list: %#v", nested)
	}
}

func TestParseErrorsAndEmptyDocument(t *testing.T) {
	if _, err := Parse(string([]byte{0xff})); err == nil {
		t.Error("invalid UTF-8 was accepted")
	}
	deep := strings.Repeat("[", maxNesting+2) + "x" + strings.Repeat("](u)", maxNesting+2)
	if _, err := Parse(deep); err == nil {
		t.Error("excessive inline nesting was accepted")
	}
	doc := mustParse(t, "")
	if len(doc.Blocks) != 0 {
		t.Errorf("empty document has %d blocks", len(doc.Blocks))
	}
}

func TestMathParsingAndLayout(t *testing.T) {
	for _, source := range []string{"x^2", `\frac{a}{b}`, `x_i^2 + \frac{1}{x^2}`, `\alpha \cdot y`} {
		if _, err := parseMath(source); err != nil {
			t.Errorf("parseMath(%q): %v", source, err)
		}
	}
	for _, source := range []string{"x^", `\frac{a}`, "{x", "x}"} {
		if _, err := parseMath(source); err == nil {
			t.Errorf("parseMath(%q) unexpectedly succeeded", source)
		}
	}
	super, _ := parseMath("x^2")
	superBox := layoutMath(super)
	if superBox.h <= ui.DefaultFont().Height() {
		t.Errorf("superscript height = %d", superBox.h)
	}
	fractionNode, _ := parseMath(`\frac{a}{b}`)
	fractionBox := layoutMath(fractionNode.children[0])
	bars := 0
	for _, part := range fractionBox.parts {
		if part.horizontalBar {
			bars++
			if part.w != fractionBox.w || part.h != 2 {
				t.Errorf("fraction bar = %#v", part)
			}
		}
	}
	if bars != 1 || fractionBox.h <= 2*ui.DefaultFont().Height() {
		t.Errorf("fraction box = %#v", fractionBox)
	}
}
