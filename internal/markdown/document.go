// Package markdown parses and renders a small, practical Markdown subset on a
// Windows 95 canvas. It deliberately has no dependencies outside the standard
// library and internal/ui.
//
// Supported syntax includes ATX headings, paragraphs, bold, italic, code spans,
// fenced code, pipe tables, nested ordered/unordered lists, and links. Inline
// math uses $...$ or \(...\) delimiters and supports superscripts, subscripts,
// and \frac{a}{b}; links display their label rather than their destination.
package markdown

// Document is a parsed Markdown document. Its zero value is an empty document.
// A Document is read-only after Parse returns and can be rendered repeatedly at
// different sizes and scroll offsets.
type Document struct {
	Blocks []Block
}

// BlockKind identifies a top-level or nested Markdown block.
type BlockKind uint8

const (
	BlockParagraph BlockKind = iota
	BlockHeading
	BlockCode
	BlockTable
	BlockList
)

// Block is one parsed block. Only the fields relevant to Kind are populated.
type Block struct {
	Kind     BlockKind
	Level    int              // Heading level, 1 through 6.
	Inlines  []Inline         // Paragraph or heading contents.
	Language string           // First word of a fenced code block's info string.
	Code     string           // Fenced code, without the fences or final newline.
	Header   TableRow         // Table header cells.
	Rows     []TableRow       // Table body rows.
	Align    []TableAlignment // Column alignments from the delimiter row.
	Ordered  bool             // Whether a list uses numbers instead of bullets.
	Start    int              // First number of an ordered list (one-based).
	Items    []ListItem       // List items, which may contain nested blocks.
}

// TableAlignment is the horizontal alignment of a table column.
type TableAlignment uint8

const (
	AlignLeft TableAlignment = iota
	AlignCenter
	AlignRight
)

// TableRow contains one inline sequence per table cell.
type TableRow [][]Inline

// ListItem is a list item's blocks. Typically the first block is a paragraph;
// subsequent blocks can be nested lists or fenced code blocks.
type ListItem struct {
	Blocks []Block
}

// InlineKind identifies an inline Markdown construct.
type InlineKind uint8

const (
	InlineText InlineKind = iota
	InlineBold
	InlineItalic
	InlineCode
	InlineLink
	InlineMath
	InlineBreak
)

// Inline is one inline node. Text holds literal text, code, or math source;
// URL holds a link destination; Children holds styled or linked contents.
// Links retain their destination in the AST but the renderer only shows text.
type Inline struct {
	Kind     InlineKind
	Text     string
	URL      string
	Children []Inline
}
