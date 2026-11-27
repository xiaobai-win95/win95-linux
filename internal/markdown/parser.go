package markdown

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxNesting = 64

// Parse parses Markdown into a reusable document. Markdown is intentionally
// lenient: an unclosed fence runs to end of input and unsupported syntax is
// displayed as text. Invalid UTF-8 and excessive nesting return an error.
func Parse(text string) (Document, error) {
	if !utf8.ValidString(text) {
		return Document{}, fmt.Errorf("markdown: input is not valid UTF-8")
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	blocks, err := parseBlocks(strings.Split(text, "\n"), 0)
	if err != nil {
		return Document{}, err
	}
	return Document{Blocks: blocks}, nil
}

func parseBlocks(lines []string, depth int) ([]Block, error) {
	if depth > maxNesting {
		return nil, fmt.Errorf("markdown: nesting exceeds %d levels", maxNesting)
	}
	var blocks []Block
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		if ok, indent, marker, count, info := fenceOpen(lines[i]); ok {
			block, next := parseFence(lines, i, indent, marker, count, info)
			blocks = append(blocks, block)
			i = next
			continue
		}
		if level, text, ok := parseHeading(lines[i]); ok {
			inlines, err := parseInlines(text, 0)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, Block{Kind: BlockHeading, Level: level, Inlines: inlines})
			i++
			continue
		}
		if header, align, ok := tableStart(lines, i); ok {
			block, next, err := parseTable(lines, i, header, align)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
			i = next
			continue
		}
		if _, ok := parseListMarker(lines[i]); ok {
			block, next, err := parseList(lines, i, depth)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
			i = next
			continue
		}

		start := i
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines, i) {
			i++
		}
		text := strings.Join(lines[start:i], "\n")
		inlines, err := parseInlines(text, 0)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, Block{Kind: BlockParagraph, Inlines: inlines})
	}
	return blocks, nil
}

func startsBlock(lines []string, i int) bool {
	if strings.TrimSpace(lines[i]) == "" {
		return true
	}
	if ok, _, _, _, _ := fenceOpen(lines[i]); ok {
		return true
	}
	if _, _, ok := parseHeading(lines[i]); ok {
		return true
	}
	if _, ok := parseListMarker(lines[i]); ok {
		return true
	}
	if _, _, ok := tableStart(lines, i); ok {
		return true
	}
	return false
}

func fenceOpen(line string) (ok bool, indent int, marker byte, count int, info string) {
	indent, pos := leadingIndent(line)
	if indent > 3 || pos >= len(line) || (line[pos] != '`' && line[pos] != '~') {
		return false, 0, 0, 0, ""
	}
	marker = line[pos]
	for pos < len(line) && line[pos] == marker {
		count++
		pos++
	}
	if count < 3 {
		return false, 0, 0, 0, ""
	}
	info = strings.TrimSpace(line[pos:])
	if marker == '`' && strings.ContainsRune(info, '`') {
		return false, 0, 0, 0, ""
	}
	return true, indent, marker, count, info
}

func parseFence(lines []string, start, indent int, marker byte, count int, info string) (Block, int) {
	var code []string
	i := start + 1
	for ; i < len(lines); i++ {
		ind, pos := leadingIndent(lines[i])
		if ind <= 3 && pos < len(lines[i]) && lines[i][pos] == marker {
			n := 0
			for pos+n < len(lines[i]) && lines[i][pos+n] == marker {
				n++
			}
			if n >= count && strings.TrimSpace(lines[i][pos+n:]) == "" {
				i++
				break
			}
		}
		code = append(code, stripIndent(lines[i], indent))
	}
	language := ""
	if fields := strings.Fields(info); len(fields) > 0 {
		language = fields[0]
	}
	return Block{Kind: BlockCode, Language: language, Code: strings.Join(code, "\n")}, i
}

func parseHeading(line string) (level int, text string, ok bool) {
	indent, pos := leadingIndent(line)
	if indent > 3 || pos >= len(line) || line[pos] != '#' {
		return 0, "", false
	}
	for pos < len(line) && line[pos] == '#' {
		level++
		pos++
	}
	if level > 6 || (pos < len(line) && line[pos] != ' ' && line[pos] != '\t') {
		return 0, "", false
	}
	text = strings.TrimSpace(line[pos:])
	trimmed := strings.TrimRight(text, "#")
	if len(trimmed) != len(text) {
		before := len(trimmed) - 1
		if before >= 0 && (text[before] == ' ' || text[before] == '\t') {
			text = strings.TrimSpace(trimmed)
		}
	}
	return level, text, true
}

func leadingIndent(s string) (columns, bytes int) {
	for bytes < len(s) {
		switch s[bytes] {
		case ' ':
			columns++
			bytes++
		case '\t':
			columns += 4 - columns%4
			bytes++
		default:
			return columns, bytes
		}
	}
	return columns, bytes
}

func stripIndent(s string, want int) string {
	col, pos := 0, 0
	for pos < len(s) && col < want {
		switch s[pos] {
		case ' ':
			col++
			pos++
		case '\t':
			next := col + 4 - col%4
			if next > want {
				return strings.Repeat(" ", next-want) + s[pos+1:]
			}
			col = next
			pos++
		default:
			return s[pos:]
		}
	}
	return s[pos:]
}

type listMarker struct {
	indent       int
	contentStart int
	contentWidth int
	ordered      bool
	number       int
}

func parseListMarker(line string) (listMarker, bool) {
	indent, pos := leadingIndent(line)
	start := pos
	ordered := false
	number := 0
	if pos < len(line) && strings.ContainsRune("-*+", rune(line[pos])) {
		pos++
	} else {
		digits := pos
		for pos < len(line) && line[pos] >= '0' && line[pos] <= '9' {
			pos++
		}
		if pos == digits || pos >= len(line) || (line[pos] != '.' && line[pos] != ')') {
			return listMarker{}, false
		}
		n, err := strconv.Atoi(line[digits:pos])
		if err != nil || n < 1 {
			return listMarker{}, false
		}
		ordered, number = true, n
		pos++
	}
	if pos < len(line) && line[pos] != ' ' && line[pos] != '\t' {
		return listMarker{}, false
	}
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		pos++
	}
	width := 0
	for _, r := range line[start:pos] {
		if r == '\t' {
			width += 4 - (indent+width)%4
		} else {
			width++
		}
	}
	return listMarker{
		indent:       indent,
		contentStart: pos,
		contentWidth: indent + width,
		ordered:      ordered,
		number:       number,
	}, true
}

func parseList(lines []string, start, depth int) (Block, int, error) {
	first, _ := parseListMarker(lines[start])
	block := Block{Kind: BlockList, Ordered: first.ordered, Start: first.number}
	i := start
	for i < len(lines) {
		marker, ok := parseListMarker(lines[i])
		if !ok || marker.indent != first.indent || marker.ordered != first.ordered {
			break
		}
		itemLines := []string{lines[i][marker.contentStart:]}
		i++
		finished := false
		for i < len(lines) && !finished {
			line := lines[i]
			if strings.TrimSpace(line) == "" {
				j := i + 1
				for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
					j++
				}
				if j >= len(lines) {
					i = j
					finished = true
					continue
				}
				next, nextIsList := parseListMarker(lines[j])
				nextIndent, _ := leadingIndent(lines[j])
				if nextIsList && next.indent == first.indent && next.ordered == first.ordered {
					i = j
					finished = true
				} else if nextIndent > first.indent {
					itemLines = append(itemLines, "")
					i++
				} else {
					i = j
					finished = true
				}
				continue
			}
			if next, nextIsList := parseListMarker(line); nextIsList && next.indent == first.indent {
				finished = true
				continue
			}
			indent, _ := leadingIndent(line)
			if indent > first.indent {
				itemLines = append(itemLines, stripIndent(line, first.contentWidth))
				i++
				continue
			}
			// CommonMark permits an unindented lazy continuation of a paragraph.
			if !startsBlock(lines, i) {
				itemLines = append(itemLines, strings.TrimSpace(line))
				i++
				continue
			}
			finished = true
		}
		children, err := parseBlocks(itemLines, depth+1)
		if err != nil {
			return Block{}, 0, err
		}
		block.Items = append(block.Items, ListItem{Blocks: children})
	}
	return block, i, nil
}

func tableStart(lines []string, i int) (header []string, align []TableAlignment, ok bool) {
	if i+1 >= len(lines) || !hasTablePipe(lines[i]) {
		return nil, nil, false
	}
	header, ok = splitTableRow(lines[i])
	if !ok || len(header) == 0 {
		return nil, nil, false
	}
	delimiter, ok := splitTableRow(lines[i+1])
	if !ok || len(delimiter) != len(header) {
		return nil, nil, false
	}
	align = make([]TableAlignment, len(delimiter))
	for i, cell := range delimiter {
		alignment, valid := parseTableAlignment(cell)
		if !valid {
			return nil, nil, false
		}
		align[i] = alignment
	}
	return header, align, true
}

func parseTable(lines []string, start int, header []string, align []TableAlignment) (Block, int, error) {
	block := Block{Kind: BlockTable, Align: align}
	headerRow, err := parseTableRow(header)
	if err != nil {
		return Block{}, 0, err
	}
	block.Header = headerRow
	i := start + 2
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" && hasTablePipe(lines[i]) {
		cells, ok := splitTableRow(lines[i])
		if !ok {
			break
		}
		row, err := parseTableRow(cells)
		if err != nil {
			return Block{}, 0, err
		}
		block.Rows = append(block.Rows, row)
		i++
	}
	return block, i, nil
}

func parseTableRow(cells []string) (TableRow, error) {
	row := make(TableRow, len(cells))
	for i, cell := range cells {
		inlines, err := parseInlines(strings.TrimSpace(cell), 0)
		if err != nil {
			return nil, err
		}
		row[i] = inlines
	}
	return row, nil
}

func parseTableAlignment(cell string) (TableAlignment, bool) {
	cell = strings.TrimSpace(cell)
	if len(cell) < 3 {
		return AlignLeft, false
	}
	left := cell[0] == ':'
	right := cell[len(cell)-1] == ':'
	start, end := 0, len(cell)
	if left {
		start++
	}
	if right {
		end--
	}
	if end-start < 3 {
		return AlignLeft, false
	}
	for i := start; i < end; i++ {
		if cell[i] != '-' {
			return AlignLeft, false
		}
	}
	switch {
	case left && right:
		return AlignCenter, true
	case right:
		return AlignRight, true
	default:
		return AlignLeft, true
	}
}

func hasTablePipe(line string) bool {
	ticks := 0
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) {
			i += 2
			continue
		}
		if line[i] == '`' {
			n := 1
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			if ticks == 0 {
				ticks = n
			} else if ticks == n {
				ticks = 0
			}
			i += n
			continue
		}
		if line[i] == '|' && ticks == 0 {
			return true
		}
		i++
	}
	return false
}

// splitTableRow ignores escaped pipes and pipes inside code spans. It keeps
// escapes in the cell text so the inline parser can turn \| into a literal |.
func splitTableRow(line string) ([]string, bool) {
	var cells []string
	start, ticks := 0, 0
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) {
			i += 2
			continue
		}
		if line[i] == '`' {
			n := 1
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			if ticks == 0 {
				ticks = n
			} else if ticks == n {
				ticks = 0
			}
			i += n
			continue
		}
		if line[i] == '|' && ticks == 0 {
			cells = append(cells, line[start:i])
			start = i + 1
		}
		i++
	}
	if ticks != 0 {
		return nil, false
	}
	cells = append(cells, line[start:])
	if len(cells) > 1 && strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	if len(cells) > 1 && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells, len(cells) > 0
}
