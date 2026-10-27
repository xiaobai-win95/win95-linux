package ui

import (
	_ "embed"
	"encoding/binary"
	"sync"
)

//go:embed fontdata.bin
var fontData []byte

type glyph struct {
	w  int
	h  int
	bm [32]byte
}

type Font struct {
	glyphs map[rune]glyph
}

var (
	fontOnce sync.Once
	fontInst *Font
)

func DefaultFont() *Font {
	fontOnce.Do(func() {
		fontInst = loadFont()
	})
	return fontInst
}

func loadFont() *Font {
	f := &Font{glyphs: make(map[rune]glyph, 24000)}
	if len(fontData) < 12 || string(fontData[:4]) != "W95F" {
		return f
	}
	count := int(binary.LittleEndian.Uint32(fontData[8:]))
	off := 12
	for i := 0; i < count && off+40 <= len(fontData); i++ {
		e := fontData[off : off+40]
		off += 40
		cp := rune(binary.LittleEndian.Uint32(e[0:]))
		var g glyph
		g.w = int(e[4]) * 8
		g.h = int(e[5])
		copy(g.bm[:], e[8:40])
		f.glyphs[cp] = g
	}
	return f
}

func (f *Font) glyphFor(r rune) glyph {
	if g, ok := f.glyphs[r]; ok {
		return g
	}
	if g, ok := f.glyphs['?']; ok {
		return g
	}
	return glyph{w: 8, h: 16}
}

// Width returns the advance width of s in pixels.
func (f *Font) Width(s string) int {
	w := 0
	for _, r := range s {
		w += f.glyphFor(r).w
	}
	return w
}

func (f *Font) Height() int { return 16 }

func (f *Font) drawGlyph(c *Canvas, x, y int, g glyph, col uint32) {
	if g.w != 8 {
		g.w = 16
	}
	for row := 0; row < g.h; row++ {
		hi := g.bm[row*2]
		lo := g.bm[row*2+1]
		bits := uint16(hi)<<8 | uint16(lo)
		for colIdx := 0; colIdx < g.w; colIdx++ {
			if bits&(0x8000>>uint(colIdx)) != 0 {
				c.Set(x+colIdx, y+row, col)
			}
		}
	}
}

// Text draws a string at x,y (top-left) with a 16px cell height.
func (f *Font) Text(c *Canvas, x, y int, s string, col uint32) {
	for _, r := range s {
		g := f.glyphFor(r)
		f.drawGlyph(c, x, y, g, col)
		x += g.w
	}
}

// TextBold draws text twice with a one pixel offset to fake a bold face.
func (f *Font) TextBold(c *Canvas, x, y int, s string, col uint32) {
	f.Text(c, x, y, s, col)
	f.Text(c, x+1, y, s, col)
}

// Scaled draws text at an integer pixel scale.
func (f *Font) Scaled(c *Canvas, x, y int, s string, col uint32, scale int) {
	if scale < 1 {
		scale = 1
	}
	for _, r := range s {
		g := f.glyphFor(r)
		for row := 0; row < g.h; row++ {
			bits := uint16(g.bm[row*2])<<8 | uint16(g.bm[row*2+1])
			for colIdx := 0; colIdx < g.w; colIdx++ {
				if bits&(0x8000>>uint(colIdx)) == 0 {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						c.Set(x+colIdx*scale+dx, y+row*scale+dy, col)
					}
				}
			}
		}
		x += g.w * scale
	}
}

// ScaledWidth returns the pixel width of s at the given scale.
func (f *Font) ScaledWidth(s string, scale int) int { return f.Width(s) * scale }

// TextClip draws s, truncating it with an ellipsis so it fits within maxW.
func (f *Font) TextClip(c *Canvas, x, y, maxW int, s string, col uint32) int {
	if maxW <= 0 {
		return 0
	}
	if f.Width(s) <= maxW {
		f.Text(c, x, y, s, col)
		return f.Width(s)
	}
	ell := "…"
	ew := f.Width(ell)
	var out []rune
	w := 0
	for _, r := range s {
		rw := f.glyphFor(r).w
		if w+rw+ew > maxW {
			break
		}
		out = append(out, r)
		w += rw
	}
	t := string(out) + ell
	f.Text(c, x, y, t, col)
	return f.Width(t)
}

// RuneWidth returns the advance width of a single rune.
func (f *Font) RuneWidth(r rune) int { return f.glyphFor(r).w }

// TextRight draws s ending at right.
func (f *Font) TextRight(c *Canvas, right, y int, s string, col uint32) {
	f.Text(c, right-f.Width(s), y, s, col)
}
