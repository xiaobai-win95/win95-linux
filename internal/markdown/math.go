package markdown

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"win95linux/internal/ui"
)

type mathNodeKind uint8

const (
	mathText mathNodeKind = iota
	mathRow
	mathSuperscript
	mathSubscript
	mathFraction
)

type mathNode struct {
	kind     mathNodeKind
	text     string
	children []mathNode
}

type mathParser struct {
	src string
	pos int
}

func validMath(src string) bool {
	_, err := parseMath(src)
	return err == nil
}

func parseMath(src string) (mathNode, error) {
	p := mathParser{src: src}
	node, err := p.sequence(false)
	if err != nil {
		return mathNode{}, err
	}
	if p.pos != len(src) {
		return mathNode{}, fmt.Errorf("markdown: unexpected %q in math", src[p.pos:])
	}
	return node, nil
}

func (p *mathParser) sequence(stopAtBrace bool) (mathNode, error) {
	var children []mathNode
	for p.pos < len(p.src) {
		if p.src[p.pos] == '}' {
			if !stopAtBrace {
				return mathNode{}, fmt.Errorf("markdown: unmatched closing brace in math")
			}
			p.pos++
			return mathNode{kind: mathRow, children: children}, nil
		}
		if unicode.IsSpace(rune(p.src[p.pos])) {
			p.pos++
			children = append(children, mathNode{kind: mathText, text: " "})
			continue
		}
		if p.src[p.pos] == '^' || p.src[p.pos] == '_' {
			if len(children) == 0 {
				return mathNode{}, fmt.Errorf("markdown: math script has no base")
			}
			kind := mathSuperscript
			if p.src[p.pos] == '_' {
				kind = mathSubscript
			}
			p.pos++
			arg, err := p.group()
			if err != nil {
				return mathNode{}, err
			}
			children[len(children)-1] = mathNode{kind: kind, children: []mathNode{children[len(children)-1], arg}}
			continue
		}
		node, err := p.primary()
		if err != nil {
			return mathNode{}, err
		}
		children = append(children, node)
	}
	if stopAtBrace {
		return mathNode{}, fmt.Errorf("markdown: unclosed brace in math")
	}
	return mathNode{kind: mathRow, children: children}, nil
}

func (p *mathParser) group() (mathNode, error) {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return mathNode{}, fmt.Errorf("markdown: missing math argument")
	}
	if p.src[p.pos] == '{' {
		p.pos++
		return p.sequence(true)
	}
	return p.primary()
}

func (p *mathParser) primary() (mathNode, error) {
	if p.pos >= len(p.src) {
		return mathNode{}, fmt.Errorf("markdown: missing math operand")
	}
	if p.src[p.pos] == '{' {
		p.pos++
		return p.sequence(true)
	}
	if p.src[p.pos] != '\\' {
		r, size := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += size
		return mathNode{kind: mathText, text: string(r)}, nil
	}

	p.pos++
	if p.pos >= len(p.src) {
		return mathNode{kind: mathText, text: `\`}, nil
	}
	start := p.pos
	for p.pos < len(p.src) {
		r, size := utf8.DecodeRuneInString(p.src[p.pos:])
		if !unicode.IsLetter(r) {
			break
		}
		p.pos += size
	}
	command := p.src[start:p.pos]
	if command == "" {
		r, size := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += size
		return mathNode{kind: mathText, text: string(r)}, nil
	}
	switch command {
	case "frac":
		numerator, err := p.group()
		if err != nil {
			return mathNode{}, err
		}
		denominator, err := p.group()
		if err != nil {
			return mathNode{}, err
		}
		return mathNode{kind: mathFraction, children: []mathNode{numerator, denominator}}, nil
	case "text":
		return p.group()
	}
	if text, ok := mathSymbols[command]; ok {
		return mathNode{kind: mathText, text: text}, nil
	}
	// Unknown commands remain readable rather than disappearing.
	return mathNode{kind: mathText, text: command}, nil
}

var mathSymbols = map[string]string{
	"alpha": "a", "beta": "b", "gamma": "g", "delta": "d",
	"theta": "th", "lambda": "l", "mu": "m", "pi": "pi",
	"cdot": "*", "times": "x", "div": "/", "pm": "+/-",
	"le": "<=", "leq": "<=", "ge": ">=", "geq": ">=",
	"neq": "!=", "infty": "inf", "sum": "SUM", "int": "INT",
}

type mathBox struct {
	w, h  int
	parts []mathPart
}

type mathPart struct {
	x, y          int
	w, h          int
	text          string
	box           *mathBox
	horizontalBar bool
}

func layoutMath(node mathNode) mathBox {
	font := ui.DefaultFont()
	switch node.kind {
	case mathText:
		return mathBox{w: font.Width(node.text), h: font.Height(), parts: []mathPart{{w: font.Width(node.text), h: font.Height(), text: node.text}}}
	case mathSuperscript, mathSubscript:
		base := layoutMath(node.children[0])
		script := layoutMath(node.children[1])
		if node.kind == mathSuperscript {
			shift := script.h + 2
			return mathBox{
				w: base.w + 2 + script.w,
				h: max(base.h+shift, script.h),
				parts: []mathPart{
					{x: 0, y: shift, box: &base},
					{x: base.w + 2, y: 0, box: &script},
				},
			}
		}
		return mathBox{
			w: base.w + 2 + script.w,
			h: base.h + 1 + script.h,
			parts: []mathPart{
				{x: 0, y: 0, box: &base},
				{x: base.w + 2, y: base.h + 1, box: &script},
			},
		}
	case mathFraction:
		numerator := layoutMath(node.children[0])
		denominator := layoutMath(node.children[1])
		w := max(numerator.w, denominator.w) + 8
		h := numerator.h + 6 + denominator.h
		return mathBox{
			w: w,
			h: h,
			parts: []mathPart{
				{x: (w - numerator.w) / 2, y: 0, box: &numerator},
				{x: 0, y: numerator.h + 2, w: w, h: 2, horizontalBar: true},
				{x: (w - denominator.w) / 2, y: numerator.h + 6, box: &denominator},
			},
		}
	default:
		box := mathBox{}
		for _, child := range node.children {
			part := layoutMath(child)
			box.parts = append(box.parts, mathPart{x: box.w, y: 0, box: &part})
			box.w += part.w
			box.h = max(box.h, part.h)
		}
		return box
	}
}

func drawMath(c *ui.Canvas, x, y int, box mathBox, col uint32) {
	font := ui.DefaultFont()
	for _, part := range box.parts {
		if part.box != nil {
			drawMath(c, x+part.x, y+part.y, *part.box, col)
			continue
		}
		if part.horizontalBar {
			c.FillRect(ui.Rect{X: x + part.x, Y: y + part.y, W: part.w, H: part.h}, col)
			continue
		}
		if part.text != "" {
			font.Text(c, x+part.x, y+part.y, part.text, col)
		}
	}
}
