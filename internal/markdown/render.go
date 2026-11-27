package markdown

import (
	"strconv"

	"win95linux/internal/ui"
)

const (
	documentPadding  = 8
	codeLineHeight   = 20
	codeHeaderHeight = 24
	tableCellPadding = 6
)

// Options controls rendering. ScrollOffset is a vertical document offset in
// pixels; negative values are treated as zero and values past the end are
// clamped. CopyCode enables the code-block buttons. Render itself never invokes
// it: call RenderResult.Activate with a click position to do that.
type Options struct {
	CopyCode     func(string)
	ScrollOffset int
}

// CodeButton describes a visible fenced-code copy button. Rect is in canvas
// coordinates and is clipped to the rendered viewport. Code is the exact text
// passed to Options.CopyCode when the button is activated.
type CodeButton struct {
	Rect ui.Rect
	Code string
}

// RenderResult describes the completed layout. ContentRect is the full
// document bounds in canvas coordinates, including scrolled-out content;
// Viewport is the part of r that was actually painted. MaxScroll is the largest
// useful ScrollOffset. CopyButtons contains only visible copy controls.
type RenderResult struct {
	ContentHeight int
	ContentRect   ui.Rect
	Viewport      ui.Rect
	MaxScroll     int
	CopyButtons   []CodeButton

	copyCode func(string)
}

// Activate invokes the copy callback when x,y is inside a visible code button.
// It returns false for other positions or when Options.CopyCode was nil.
func (result RenderResult) Activate(x, y int) bool {
	if result.copyCode == nil {
		return false
	}
	for _, button := range result.CopyButtons {
		if button.Rect.Contains(x, y) {
			result.copyCode(button.Code)
			return true
		}
	}
	return false
}

// Render draws doc into r using the classic UI palette and ui.DefaultFont. All
// drawing is clipped to r and the canvas, so neighboring desktop content is
// safe. The returned rectangles can be used for scrolling and click handling.
func Render(c *ui.Canvas, r ui.Rect, doc Document, opts Options) RenderResult {
	if c == nil || r.W <= 0 || r.H <= 0 {
		return RenderResult{}
	}
	x0, y0 := max(0, r.X), max(0, r.Y)
	x1, y1 := min(c.W, r.Right()), min(c.H, r.Bottom())
	if x0 >= x1 || y0 >= y1 || c.Stride <= 0 {
		return RenderResult{}
	}
	viewW, viewH := x1-x0, y1-y0
	start := y0*c.Stride + x0
	end := start + (viewH-1)*c.Stride + viewW
	if start < 0 || end > len(c.Pix) {
		return RenderResult{}
	}
	view := &ui.Canvas{W: viewW, H: viewH, Stride: c.Stride, Pix: c.Pix[start:end]}
	view.FillRect(ui.Rect{W: viewW, H: viewH}, ui.ColField)

	offset := max(0, opts.ScrollOffset)
	layoutX, layoutY := r.X-x0, r.Y-y0
	layout := func(scroll int) (int, []copyTarget) {
		ctx := renderContext{c: view, opts: opts}
		y := layoutY + documentPadding - scroll
		y = renderBlocks(&ctx, layoutX, y, r.W, doc.Blocks)
		return y - layoutY + scroll + documentPadding, ctx.buttons
	}
	contentHeight, buttons := layout(offset)
	maxScroll := max(0, contentHeight-r.H)
	if offset > maxScroll {
		offset = maxScroll
		view.FillRect(ui.Rect{W: viewW, H: viewH}, ui.ColField)
		contentHeight, buttons = layout(offset)
	}
	result := RenderResult{
		ContentHeight: contentHeight,
		ContentRect:   ui.Rect{X: r.X, Y: r.Y - offset, W: r.W, H: contentHeight},
		Viewport:      ui.Rect{X: x0, Y: y0, W: viewW, H: viewH},
		MaxScroll:     maxScroll,
		copyCode:      opts.CopyCode,
	}
	for _, target := range buttons {
		visible := clipRect(target.rect, ui.Rect{W: viewW, H: viewH})
		if visible.W <= 0 || visible.H <= 0 {
			continue
		}
		result.CopyButtons = append(result.CopyButtons, CodeButton{
			Rect: ui.Rect{X: x0 + visible.X, Y: y0 + visible.Y, W: visible.W, H: visible.H},
			Code: target.code,
		})
	}
	return result
}

func clipRect(r, viewport ui.Rect) ui.Rect {
	x0, y0 := max(viewport.X, r.X), max(viewport.Y, r.Y)
	x1, y1 := min(viewport.Right(), r.Right()), min(viewport.Bottom(), r.Bottom())
	return ui.Rect{X: x0, Y: y0, W: max(0, x1-x0), H: max(0, y1-y0)}
}

type copyTarget struct {
	rect ui.Rect
	code string
}

type renderContext struct {
	c       *ui.Canvas
	opts    Options
	buttons []copyTarget
}

func renderBlocks(ctx *renderContext, x, y, width int, blocks []Block) int {
	for _, block := range blocks {
		y = renderBlock(ctx, x, y, width, block)
	}
	return y
}

func renderBlock(ctx *renderContext, x, y, width int, block Block) int {
	switch block.Kind {
	case BlockHeading:
		scale := 1
		if block.Level == 1 {
			scale = 2
		}
		y += 2
		lines := layoutInline(block.Inlines, width, textStyle{bold: true, scale: scale})
		drawInlineLines(ctx.c, x, y, width, lines, AlignLeft)
		y += inlineHeight(lines)
		if block.Level <= 2 {
			ctx.c.HLine(x, y+3, width, ui.ColShadow)
			y += 7
		} else {
			y += 4
		}
		return y + 6
	case BlockCode:
		return renderCodeBlock(ctx, x, y, width, block)
	case BlockTable:
		return renderTable(ctx, x, y, width, block)
	case BlockList:
		return renderList(ctx, x, y, width, block)
	default:
		lines := layoutInline(block.Inlines, width, textStyle{scale: 1})
		drawInlineLines(ctx.c, x, y, width, lines, AlignLeft)
		return y + inlineHeight(lines) + 10
	}
}

func renderCodeBlock(ctx *renderContext, x, y, width int, block Block) int {
	y += 4
	width = max(1, width)
	innerWidth := max(1, width-14)
	lines := codeDisplayLines(block.Code, innerWidth)
	height := codeHeaderHeight + 14 + len(lines)*codeLineHeight
	outer := ui.Rect{X: x, Y: y, W: width, H: height}
	ctx.c.FillRect(outer, ui.ColField)
	ctx.c.Sunken(outer)
	ctx.c.FillRect(ui.Rect{X: x + 2, Y: y + 2, W: max(0, width-4), H: codeHeaderHeight}, ui.ColFace)
	ctx.c.HLine(x+2, y+1+codeHeaderHeight, max(0, width-4), ui.ColShadow)

	buttonW := 52
	if width < 64 {
		buttonW = max(1, width-12)
	}
	button := ui.Rect{X: x + width - 6 - buttonW, Y: y + 5, W: buttonW, H: 18}
	label := block.Language
	if label == "" {
		label = "Code"
	}
	labelWidth := max(0, button.X-(x+8)-4)
	ui.DefaultFont().TextClip(ctx.c, x+8, y+6, labelWidth, label, ui.ColText)
	ui.Button(ctx.c, button, "Copy", false, ctx.opts.CopyCode != nil)
	ctx.buttons = append(ctx.buttons, copyTarget{rect: button, code: block.Code})

	textY := y + 2 + codeHeaderHeight + 6
	for _, line := range lines {
		ui.DefaultFont().Text(ctx.c, x+8, textY, line, ui.ColText)
		textY += codeLineHeight
	}
	return y + height + 10
}

type tableCellLayout struct {
	lines []inlineLine
	align TableAlignment
}

type tableLayout struct {
	widths []int
	rows   [][]tableCellLayout
	height int
}

func renderTable(ctx *renderContext, x, y, width int, block Block) int {
	y += 4
	if len(block.Header) == 0 {
		return y + 6
	}
	layout := layoutTable(block, max(1, width-2))
	ctx.c.FillRect(ui.Rect{X: x, Y: y, W: width, H: layout.height}, ui.ColField)
	ctx.c.Frame(ui.Rect{X: x, Y: y, W: width, H: layout.height}, ui.ColDark)
	rowY := y + 1
	for rowIndex, row := range layout.rows {
		rowHeight := tableRowHeight(row)
		if rowIndex == 0 {
			ctx.c.FillRect(ui.Rect{X: x + 1, Y: rowY, W: max(0, width-2), H: rowHeight}, ui.ColFace)
		}
		cellX := x + 1
		for col, cell := range row {
			cellWidth := layout.widths[col]
			align := cell.align
			if col < len(block.Align) {
				align = block.Align[col]
			}
			drawInlineLines(ctx.c, cellX+tableCellPadding, rowY+tableCellPadding,
				max(1, cellWidth-2*tableCellPadding), cell.lines, align)
			cellX += cellWidth
			if col+1 < len(row) {
				ctx.c.VLine(cellX, rowY, rowHeight, ui.ColShadow)
			}
		}
		ctx.c.HLine(x+1, rowY+rowHeight-1, max(0, width-2), ui.ColShadow)
		rowY += rowHeight
	}
	return y + layout.height + 10
}

func layoutTable(block Block, width int) tableLayout {
	columns := len(block.Header)
	width = max(1, width)
	desired := make([]int, columns)
	minimum := min(24, max(1, width/columns))
	style := textStyle{scale: 1}
	measureRow := func(row TableRow, header bool) {
		cellStyle := style
		if header {
			cellStyle.bold = true
		}
		for col := 0; col < columns; col++ {
			var inlines []Inline
			if col < len(row) {
				inlines = row[col]
			}
			natural := naturalInlineWidth(inlines, cellStyle) + 2*tableCellPadding
			desired[col] = max(desired[col], minimum, natural)
		}
	}
	measureRow(block.Header, true)
	for _, row := range block.Rows {
		measureRow(row, false)
	}
	widths := distributeWidths(desired, minimum, width)
	layout := tableLayout{widths: widths, height: 2}
	appendRow := func(row TableRow, header bool) {
		cells := make([]tableCellLayout, columns)
		cellStyle := style
		if header {
			cellStyle.bold = true
		}
		for col := 0; col < columns; col++ {
			var inlines []Inline
			if col < len(row) {
				inlines = row[col]
			}
			align := AlignLeft
			if col < len(block.Align) {
				align = block.Align[col]
			}
			cells[col] = tableCellLayout{
				lines: layoutInline(inlines, max(1, widths[col]-2*tableCellPadding), cellStyle),
				align: align,
			}
		}
		layout.rows = append(layout.rows, cells)
		layout.height += tableRowHeight(cells)
	}
	appendRow(block.Header, true)
	for _, row := range block.Rows {
		appendRow(row, false)
	}
	return layout
}

func distributeWidths(desired []int, minimum, width int) []int {
	widths := make([]int, len(desired))
	total := 0
	for _, value := range desired {
		total += value
	}
	if minimum*len(desired) > width {
		for i := range widths {
			widths[i] = width / len(widths)
		}
		for i := 0; i < width%len(widths); i++ {
			widths[i]++
		}
		return widths
	}
	if total > width {
		extraTotal := total - minimum*len(desired)
		targetExtra := width - minimum*len(desired)
		for i, value := range desired {
			extra := value - minimum
			widths[i] = minimum
			if extraTotal > 0 {
				widths[i] += extra * targetExtra / extraTotal
			}
		}
	} else {
		copy(widths, desired)
	}
	used := 0
	for _, value := range widths {
		used += value
	}
	extra := max(0, width-used) / len(widths)
	remainder := max(0, width-used) % len(widths)
	for i := range widths {
		widths[i] += extra
		if i < remainder {
			widths[i]++
		}
	}
	return widths
}

func naturalInlineWidth(nodes []Inline, style textStyle) int {
	atoms := buildInlineAtoms(nodes, style)
	width := 0
	for _, atom := range atoms {
		if atom.kind == atomBreak {
			continue
		}
		width += atom.width
		if atom.spaceBefore {
			width += ui.DefaultFont().RuneWidth(' ') * atom.style.scale
		}
	}
	return width
}

func tableRowHeight(row []tableCellLayout) int {
	height := 28
	for _, cell := range row {
		height = max(height, inlineHeight(cell.lines)+2*tableCellPadding)
	}
	return height
}

func renderList(ctx *renderContext, x, y, width int, block Block) int {
	y += 2
	lastNumber := block.Start + len(block.Items) - 1
	markerWidth := 20
	if block.Ordered {
		markerWidth = max(markerWidth, ui.DefaultFont().Width(strconv.Itoa(lastNumber)+".")+8)
	}
	for i, item := range block.Items {
		if block.Ordered {
			label := strconv.Itoa(block.Start+i) + "."
			ui.DefaultFont().TextRight(ctx.c, x+markerWidth-6, y+2, label, ui.ColText)
		} else {
			ctx.c.FillRect(ui.Rect{X: x + 5, Y: y + 8, W: 5, H: 5}, ui.ColNavy)
		}
		if len(item.Blocks) == 0 {
			y += codeLineHeight
		} else {
			y = renderBlocks(ctx, x+markerWidth, y, max(1, width-markerWidth), item.Blocks)
		}
		y += 2
	}
	return y + 6
}
