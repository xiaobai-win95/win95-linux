package ui

// Button draws a classic Windows 95 push button.
func Button(c *Canvas, r Rect, label string, pressed, enabled bool) {
	c.FillRect(r, ColFace)
	if pressed {
		c.HLine(r.X, r.Y, r.W, ColDark)
		c.VLine(r.X, r.Y, r.H, ColDark)
		c.HLine(r.X+1, r.Y+1, r.W-1, ColShadow)
		c.VLine(r.X+1, r.Y+1, r.H-1, ColShadow)
		c.HLine(r.X, r.Bottom()-1, r.W, ColWhite)
		c.VLine(r.Right()-1, r.Y, r.H, ColWhite)
		c.HLine(r.X+1, r.Bottom()-2, r.W-1, ColLight)
		c.VLine(r.Right()-2, r.Y+1, r.H-1, ColLight)
	} else {
		c.HLine(r.X, r.Y, r.W, ColWhite)
		c.VLine(r.X, r.Y, r.H, ColWhite)
		c.HLine(r.X+1, r.Y+1, r.W-2, ColLight)
		c.VLine(r.X+1, r.Y+1, r.H-2, ColLight)
		c.HLine(r.X, r.Bottom()-1, r.W, ColDark)
		c.VLine(r.Right()-1, r.Y, r.H, ColDark)
		c.HLine(r.X+1, r.Bottom()-2, r.W-2, ColShadow)
		c.VLine(r.Right()-2, r.Y+1, r.H-2, ColShadow)
	}
	f := DefaultFont()
	col := ColText
	if !enabled {
		col = ColTextDis
	}
	tx := r.X + (r.W-f.Width(label))/2
	ty := r.Y + (r.H-f.Height())/2
	if pressed {
		tx++
		ty++
	}
	f.Text(c, tx, ty, label, col)
}

// GroupBox draws a group box with a label notch.
func GroupBox(c *Canvas, r Rect, label string) {
	f := DefaultFont()
	c.HLine(r.X+1, r.Y, r.W-2, ColShadow)
	c.HLine(r.X+1, r.Y+1, r.W-2, ColWhite)
	c.VLine(r.X, r.Y+1, r.H-2, ColShadow)
	c.VLine(r.X+1, r.Y+1, r.H-2, ColWhite)
	c.HLine(r.X+1, r.Bottom()-2, r.W-2, ColShadow)
	c.HLine(r.X+1, r.Bottom()-1, r.W-2, ColWhite)
	c.VLine(r.Right()-2, r.Y+1, r.H-2, ColShadow)
	c.VLine(r.Right()-1, r.Y+1, r.H-2, ColWhite)
	lw := f.Width(label)
	c.FillRect(Rect{r.X + 8, r.Y, lw + 4, 2}, ColFace)
	f.Text(c, r.X+10, r.Y-8, label, ColText)
}

// CheckMark draws a green check mark in a 16x16 cell.
func CheckMark(c *Canvas, x, y int, col uint32) {
	pts := [][2]int{
		{2, 8}, {3, 9}, {4, 10}, {5, 11}, {6, 10}, {7, 9}, {8, 8},
		{9, 7}, {10, 6}, {11, 5}, {12, 4},
	}
	for _, p := range pts {
		c.Set(x+p[0], y+p[1], col)
		c.Set(x+p[0], y+p[1]+1, col)
	}
}

// CrossMark draws an X mark in a 16x16 cell.
func CrossMark(c *Canvas, x, y int, col uint32) {
	for i := 3; i < 13; i++ {
		c.Set(x+i, y+i, col)
		c.Set(x+15-i, y+i, col)
	}
}

// QuestionMark draws a question mark badge.
func QuestionMark(c *Canvas, x, y int, col uint32) {
	for i := 5; i < 11; i++ {
		c.Set(x+i, y+3, col)
		c.Set(x+i, y+4, col)
	}
	for i := 4; i < 9; i++ {
		c.Set(x+10, y+i, col)
	}
	c.Set(x+9, y+8, col)
	c.Set(x+8, y+9, col)
	c.Set(x+8, y+10, col)
	c.Set(x+8, y+13, col)
	c.Set(x+8, y+14, col)
}

var cursorRows = []string{
	"X...........",
	"XX..........",
	"X#X.........",
	"X##X........",
	"X###X.......",
	"X####X......",
	"X#####X.....",
	"X######X....",
	"X#######X...",
	"X########X..",
	"X#####XXXXX.",
	"X##X#X......",
	"X#X.X#X.....",
	"XX..X#X.....",
	"X....X#X....",
	".....X#X....",
	"......X#X...",
	"......X#X...",
	".......XX...",
}

// Cursor draws the standard arrow pointer with the hotspot at x,y.
func Cursor(c *Canvas, x, y int) {
	for row, s := range cursorRows {
		for col, ch := range s {
			switch ch {
			case 'X':
				c.Set(x+col, y+row, ColDark)
			case '#':
				c.Set(x+col, y+row, ColWhite)
			}
		}
	}
}

// IconComputer draws a small desktop computer icon.
func IconComputer(c *Canvas, x, y, scale int, selected bool) {
	// 32x32 design scaled up.
	set := func(xx, yy int, col uint32) {
		c.FillRect(Rect{x + xx*scale, y + yy*scale, scale, scale}, col)
	}
	if selected {
		for yy := 0; yy < 32; yy++ {
			for xx := 0; xx < 32; xx++ {
				if (xx+yy)&1 == 0 {
					set(xx, yy, ColNavy)
				}
			}
		}
	}
	// monitor
	for yy := 3; yy < 23; yy++ {
		for xx := 3; xx < 29; xx++ {
			set(xx, yy, ColLight)
		}
	}
	for xx := 3; xx < 29; xx++ {
		set(xx, 3, ColWhite)
		set(xx, 22, ColDark)
	}
	for yy := 3; yy < 23; yy++ {
		set(3, yy, ColWhite)
		set(28, yy, ColDark)
	}
	// screen
	for yy := 6; yy < 19; yy++ {
		for xx := 6; xx < 26; xx++ {
			set(xx, yy, ColDesktop)
		}
	}
	for xx := 7; xx < 12; xx++ {
		set(xx, 7, 0x40C0C0)
	}
	set(7, 8, 0x40C0C0)
	set(11, 8, 0x40C0C0)
	// stand and base
	for xx := 12; xx < 20; xx++ {
		set(xx, 23, ColShadow)
		set(xx, 24, ColShadow)
	}
	for xx := 9; xx < 23; xx++ {
		set(xx, 25, ColLight)
		set(xx, 26, ColShadow)
	}
}

// IconRecycle draws the recycle bin icon.
func IconRecycle(c *Canvas, x, y, scale int) {
	set := func(xx, yy int, col uint32) {
		c.FillRect(Rect{x + xx*scale, y + yy*scale, scale, scale}, col)
	}
	for yy := 8; yy < 27; yy++ {
		for xx := 8; xx < 24; xx++ {
			set(xx, yy, ColLight)
		}
	}
	for xx := 8; xx < 24; xx++ {
		set(xx, 27, ColShadow)
		set(xx, 27, ColShadow)
	}
	for yy := 8; yy < 28; yy++ {
		set(8, yy, ColWhite)
		set(23, yy, ColShadow)
	}
	for yy := 6; yy < 8; yy++ {
		for xx := 7; xx < 25; xx++ {
			set(xx, yy, ColShadow)
		}
	}
	for xx := 12; xx < 20; xx += 2 {
		for yy := 10; yy < 25; yy++ {
			set(xx, yy, ColShadow)
		}
	}
}

// IconFolder draws a folder icon.
func IconFolder(c *Canvas, x, y, scale int) {
	set := func(xx, yy int, col uint32) {
		c.FillRect(Rect{x + xx*scale, y + yy*scale, scale, scale}, col)
	}
	for yy := 8; yy < 25; yy++ {
		for xx := 4; xx < 28; xx++ {
			set(xx, yy, 0xFFD040)
		}
	}
	for yy := 6; yy < 9; yy++ {
		for xx := 4; xx < 14; xx++ {
			set(xx, yy, 0xFFD040)
		}
	}
	for xx := 4; xx < 28; xx++ {
		set(xx, 8, 0xFFF0A0)
		set(xx, 24, 0x806000)
	}
	set(27, 9, 0x806000)
	for xx := 4; xx < 28; xx++ {
		set(xx, 25, 0x806000)
	}
}

// FlagLogo draws the four color Windows-like flag.
func FlagLogo(c *Canvas, x, y, size int) {
	q := size / 2
	if q < 1 {
		q = 1
	}
	cols := [4]uint32{0xE04040, 0x40A040, 0x4060E0, 0xE0C040}
	for i := 0; i < 4; i++ {
		ox := (i & 1) * q
		oy := (i >> 1) * q
		for yy := 0; yy < q-1; yy++ {
			shift := (yy / 3) % 2
			for xx := 1; xx < q-1; xx++ {
				c.Set(x+ox+xx+shift, y+oy+yy, cols[i])
			}
		}
	}
}

// SmallButton draws a title bar button with a glyph ("_", "□", "×").
func SmallButton(c *Canvas, r Rect, glyph string, pressed bool) {
	Button(c, r, "", pressed, true)
	switch glyph {
	case "_":
		c.FillRect(Rect{r.X + 4, r.Y + r.H - 7, r.W - 8, 2}, ColDark)
	case "□":
		c.Frame(Rect{r.X + 5, r.Y + 4, r.W - 10, r.H - 9}, ColDark)
		c.HLine(r.X+5, r.Y+5, r.W-10, ColWhite)
	case "×":
		cx := r.X + r.W/2 - 4
		cy := r.Y + r.H/2 - 4
		for i := 0; i < 8; i++ {
			c.Set(cx+i, cy+i, ColDark)
			c.Set(cx+7-i, cy+i, ColDark)
		}
	}
}

// tinyGlyphs is a 3x5 bitmap font used by the taskbar system monitor where a
// 16px cell would not fit twice.
var tinyGlyphs = map[rune][5]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "010", "010", "010"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	'%': {"101", "001", "010", "100", "101"},
	'A': {"010", "101", "111", "101", "101"},
	'C': {"111", "100", "100", "100", "111"},
	'M': {"101", "111", "111", "101", "101"},
	'P': {"111", "101", "111", "100", "100"},
	'R': {"111", "101", "111", "110", "101"},
	'U': {"101", "101", "101", "101", "111"},
	' ': {"000", "000", "000", "000", "000"},
}

// TinyWidth returns the advance width of s in the 3x5 font.
func TinyWidth(s string) int { return len([]rune(s)) * 4 }

// TinyText draws s with the 3x5 font, one pixel per bit.
func TinyText(c *Canvas, x, y int, s string, col uint32) {
	for _, r := range s {
		rows, ok := tinyGlyphs[r]
		if !ok {
			rows = tinyGlyphs[' ']
		}
		for yy, row := range rows {
			for xx, ch := range row {
				if ch == '1' {
					c.Set(x+xx, y+yy, col)
				}
			}
		}
		x += 4
	}
}
