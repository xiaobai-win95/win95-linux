package markdown

import (
	"strings"
	"unicode"

	"win95linux/internal/ui"
)

type textStyle struct {
	bold, italic, code, link bool
	scale                    int
}

func (s textStyle) normalized() textStyle {
	if s.scale < 1 {
		s.scale = 1
	}
	return s
}

type atomKind uint8

const (
	atomText atomKind = iota
	atomMath
	atomBreak
)

type inlineAtom struct {
	kind                     atomKind
	text                     string
	style                    textStyle
	box                      mathBox
	width, ascent, descent   int
	padLeft, padRight        int
	spaceBefore, forcedBreak bool
}

type inlineLine struct {
	atoms                  []inlineAtom
	width, ascent, descent int
}

func (l inlineLine) height() int {
	return l.ascent + l.descent
}

func buildInlineAtoms(nodes []Inline, base textStyle) []inlineAtom {
	base = base.normalized()
	var atoms []inlineAtom
	pendingSpace := false
	appendText := func(text string, style textStyle) {
		if text == "" {
			return
		}
		atoms = append(atoms, makeTextAtom(text, style, pendingSpace))
		pendingSpace = false
	}
	var walk func([]Inline, textStyle)
	walk = func(nodes []Inline, style textStyle) {
		for _, node := range nodes {
			switch node.Kind {
			case InlineText:
				var word strings.Builder
				flushWord := func() {
					appendText(word.String(), style)
					word.Reset()
				}
				for _, r := range node.Text {
					if unicode.IsSpace(r) {
						flushWord()
						pendingSpace = true
					} else {
						word.WriteRune(r)
					}
				}
				flushWord()
			case InlineBold:
				childStyle := style
				childStyle.bold = true
				walk(node.Children, childStyle)
			case InlineItalic:
				childStyle := style
				childStyle.italic = true
				walk(node.Children, childStyle)
			case InlineLink:
				childStyle := style
				childStyle.link = true
				walk(node.Children, childStyle)
			case InlineCode:
				childStyle := style
				childStyle.code = true
				atoms = append(atoms, inlineAtom{
					kind:        atomText,
					text:        node.Text,
					style:       childStyle,
					width:       measureText(node.Text, childStyle) + 4,
					ascent:      ui.DefaultFont().Height() * childStyle.scale,
					descent:     4 * childStyle.scale,
					padLeft:     2,
					padRight:    2,
					spaceBefore: pendingSpace,
				})
				pendingSpace = false
			case InlineMath:
				atom := inlineAtom{kind: atomMath, style: style, spaceBefore: pendingSpace}
				if math, err := parseMath(node.Text); err == nil {
					atom.box = layoutMath(math)
					atom.width = atom.box.w
					atom.ascent = max(atom.box.h, ui.DefaultFont().Height())
					atom.descent = 4
				} else {
					atom.kind = atomText
					atom.text = node.Text
					atom.width = measureText(atom.text, style)
					atom.ascent = ui.DefaultFont().Height() * style.scale
					atom.descent = 4 * style.scale
				}
				atoms = append(atoms, atom)
				pendingSpace = false
			case InlineBreak:
				atoms = append(atoms, inlineAtom{kind: atomBreak, forcedBreak: true})
				pendingSpace = false
			}
		}
	}
	walk(nodes, base)
	return atoms
}

func makeTextAtom(text string, style textStyle, spaceBefore bool) inlineAtom {
	style = style.normalized()
	return inlineAtom{
		kind:        atomText,
		text:        text,
		style:       style,
		width:       measureText(text, style),
		ascent:      ui.DefaultFont().Height() * style.scale,
		descent:     4 * style.scale,
		spaceBefore: spaceBefore,
	}
}

func measureText(text string, style textStyle) int {
	style = style.normalized()
	width := ui.DefaultFont().ScaledWidth(text, style.scale)
	if style.italic && text != "" {
		width += italicSlant(style.scale)
	}
	return width
}

func italicSlant(scale int) int {
	return 2 * scale
}

func splitWideAtom(atom inlineAtom, maxWidth int) []inlineAtom {
	if atom.kind != atomText || atom.width <= maxWidth || atom.text == "" {
		return []inlineAtom{atom}
	}
	padding := atom.padLeft + atom.padRight
	if maxWidth <= padding {
		return []inlineAtom{atom}
	}
	runes := []rune(atom.text)
	var chunks []string
	var chunk strings.Builder
	chunkWidth := 0
	flush := func() {
		if chunk.Len() > 0 {
			chunks = append(chunks, chunk.String())
			chunk.Reset()
			chunkWidth = 0
		}
	}
	for _, r := range runes {
		width := ui.DefaultFont().RuneWidth(r) * atom.style.scale
		if atom.style.italic {
			width += italicSlant(atom.style.scale)
		}
		if chunk.Len() > 0 && chunkWidth+width > maxWidth-padding {
			flush()
		}
		chunk.WriteRune(r)
		chunkWidth += width
	}
	flush()
	if len(chunks) <= 1 {
		return []inlineAtom{atom}
	}
	out := make([]inlineAtom, 0, len(chunks))
	for i, text := range chunks {
		part := makeTextAtom(text, atom.style, false)
		part.padLeft, part.padRight = 0, 0
		if i == 0 {
			part.padLeft = atom.padLeft
			part.spaceBefore = atom.spaceBefore
		}
		if i == len(chunks)-1 {
			part.padRight = atom.padRight
		}
		part.width += part.padLeft + part.padRight
		out = append(out, part)
	}
	return out
}

func layoutInline(nodes []Inline, width int, base textStyle) []inlineLine {
	base = base.normalized()
	if width < 1 {
		width = 1
	}
	atoms := buildInlineAtoms(nodes, base)
	var lines []inlineLine
	current := inlineLine{}
	flush := func() {
		if len(current.atoms) == 0 {
			current.ascent = ui.DefaultFont().Height() * base.scale
			current.descent = 4 * base.scale
		}
		lines = append(lines, current)
		current = inlineLine{}
	}
	for _, atom := range atoms {
		if atom.kind == atomBreak {
			flush()
			continue
		}
		if atom.width > width {
			for _, part := range splitWideAtom(atom, width) {
				part.spaceBefore = part.spaceBefore && len(current.atoms) > 0
				space := 0
				if part.spaceBefore {
					space = ui.DefaultFont().RuneWidth(' ') * part.style.scale
				}
				if len(current.atoms) > 0 && current.width+space+part.width > width {
					flush()
					part.spaceBefore = false
					space = 0
				}
				current.atoms = append(current.atoms, part)
				current.width += space + part.width
				current.ascent = max(current.ascent, part.ascent)
				current.descent = max(current.descent, part.descent)
			}
			continue
		}
		space := 0
		if atom.spaceBefore {
			space = ui.DefaultFont().RuneWidth(' ') * atom.style.scale
		}
		if len(current.atoms) > 0 && current.width+space+atom.width > width {
			flush()
			atom.spaceBefore = false
			space = 0
		}
		if len(current.atoms) == 0 {
			atom.spaceBefore = false
			space = 0
		}
		current.atoms = append(current.atoms, atom)
		current.width += space + atom.width
		current.ascent = max(current.ascent, atom.ascent)
		current.descent = max(current.descent, atom.descent)
	}
	flush()
	return lines
}

func inlineHeight(lines []inlineLine) int {
	height := 0
	for _, line := range lines {
		height += line.height()
	}
	return height
}

func drawInlineLines(c *ui.Canvas, x, y, width int, lines []inlineLine, align TableAlignment) int {
	for _, line := range lines {
		lineX := x
		switch align {
		case AlignCenter:
			lineX += max(0, (width-line.width)/2)
		case AlignRight:
			lineX += max(0, width-line.width)
		}
		for _, atom := range line.atoms {
			if atom.kind == atomBreak {
				continue
			}
			if atom.spaceBefore {
				lineX += ui.DefaultFont().RuneWidth(' ') * atom.style.scale
			}
			atomY := y + line.ascent - atom.ascent
			drawInlineAtom(c, lineX, atomY, atom)
			lineX += atom.width
		}
		y += line.height()
	}
	return y
}

func drawInlineAtom(c *ui.Canvas, x, y int, atom inlineAtom) {
	col := ui.ColText
	if atom.style.link {
		col = ui.ColBlue
	} else if atom.style.code {
		col = ui.ColNavy
	}
	if atom.kind == atomMath {
		drawMath(c, x, y, atom.box, col)
		if atom.style.link {
			c.HLine(x, y+atom.box.h+1, atom.box.w, ui.ColBlue)
		}
		return
	}
	if atom.style.code {
		c.FillRect(ui.Rect{X: x, Y: y - 2, W: atom.width, H: atom.ascent + atom.descent}, ui.ColLight)
	}
	drawStyledText(c, x+atom.padLeft, y, atom.text, atom.style, col)
	if atom.style.link {
		c.HLine(x, y+atom.ascent+1, atom.width, ui.ColBlue)
	}
}

func drawStyledText(c *ui.Canvas, x, y int, text string, style textStyle, col uint32) {
	style = style.normalized()
	font := ui.DefaultFont()
	if !style.italic {
		if style.scale == 1 {
			if style.bold {
				font.TextBold(c, x, y, text, col)
			} else {
				font.Text(c, x, y, text, col)
			}
			return
		}
		font.Scaled(c, x, y, text, col, style.scale)
		if style.bold {
			font.Scaled(c, x+1, y, text, col, style.scale)
		}
		return
	}

	// The bitmap font has no italic face. Render a transparent mask and shear
	// its scanlines to get a small, crisp faux italic without another font.
	slant := italicSlant(style.scale)
	width := font.ScaledWidth(text, style.scale) + slant
	height := font.Height() * style.scale
	if width <= 0 || height <= 0 || text == "" {
		return
	}
	mask := ui.NewCanvas(width, height)
	if style.scale == 1 {
		if style.bold {
			font.TextBold(mask, 0, 0, text, ui.ColWhite)
		} else {
			font.Text(mask, 0, 0, text, ui.ColWhite)
		}
	} else {
		font.Scaled(mask, 0, 0, text, ui.ColWhite, style.scale)
		if style.bold {
			font.Scaled(mask, 1, 0, text, ui.ColWhite, style.scale)
		}
	}
	for row := 0; row < height; row++ {
		shift := (height - 1 - row) * slant / height
		source := mask.Pix[row*mask.Stride : row*mask.Stride+mask.W]
		for px := 0; px < mask.W; px++ {
			if source[px] != 0 {
				c.Set(x+px+shift, y+row, col)
			}
		}
	}
}

func codeDisplayLines(code string, width int) []string {
	var lines []string
	for _, source := range strings.Split(code, "\n") {
		source = strings.ReplaceAll(source, "\t", "    ")
		runes := []rune(source)
		if len(runes) == 0 || ui.DefaultFont().Width(source) <= width {
			lines = append(lines, source)
			continue
		}
		start := 0
		for start < len(runes) {
			end, used := start, 0
			for end < len(runes) {
				w := ui.DefaultFont().RuneWidth(runes[end])
				if end > start && used+w > width {
					break
				}
				used += w
				end++
			}
			lines = append(lines, string(runes[start:end]))
			start = end
		}
	}
	return lines
}
