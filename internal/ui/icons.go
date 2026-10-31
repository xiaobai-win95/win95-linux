package ui

// IconKind identifies a Win95-style icon drawn from code.
type IconKind int

const (
	IconNone IconKind = iota
	IcoComputer
	IcoRecycle
	IcoFolder
	IcoFolderOpen
	IcoFile
	IcoText
	IcoTerminal
	IcoNotepad
	IcoBrowser
	IcoCalc
	IcoCalendar
	IcoDisk
	IcoNetwork
	IcoInfo
	IcoDownload
	IcoSetup
	IcoDrive
	IcoPackage
	IcoMine
	IcoPaint
	IcoNuke
	IcoCards
)

// DrawIcon32 draws a 32x32 icon at x,y. selected dithers the background navy.
func DrawIcon32(c *Canvas, kind IconKind, x, y int, selected bool) {
	if selected {
		for yy := 0; yy < 32; yy++ {
			for xx := 0; xx < 32; xx++ {
				if (xx+yy)&1 == 0 {
					c.Set(x+xx, y+yy, ColNavy)
				}
			}
		}
	}
	set := func(xx, yy int, col uint32) { c.Set(x+xx, y+yy, col) }
	box := func(x0, y0, x1, y1 int, col uint32) {
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				set(xx, yy, col)
			}
		}
	}
	frame := func(x0, y0, x1, y1 int, col uint32) {
		for xx := x0; xx <= x1; xx++ {
			set(xx, y0, col)
			set(xx, y1, col)
		}
		for yy := y0; yy <= y1; yy++ {
			set(x0, yy, col)
			set(x1, yy, col)
		}
	}
	switch kind {
	case IcoComputer:
		IconComputer(c, x, y, 1, false)
	case IcoRecycle:
		IconRecycle(c, x, y, 1)
	case IcoFolder, IcoFolderOpen:
		IconFolder(c, x, y, 1)
		if kind == IcoFolderOpen {
			for yy := 13; yy < 26; yy++ {
				for xx := 9; xx < 29; xx++ {
					set(xx, yy, 0xFFF0A0)
				}
			}
			frame(9, 13, 28, 25, 0x806000)
			for xx := 9; xx < 29; xx++ {
				set(xx, 14, ColWhite)
			}
		}
	case IcoDisk, IcoDrive:
		box(4, 7, 27, 25, ColFace)
		frame(4, 7, 27, 25, ColDark)
		for yy := 8; yy < 12; yy++ {
			for xx := 5; xx < 27; xx++ {
				set(xx, yy, ColShadow)
			}
		}
		box(8, 16, 23, 23, ColWhite)
		frame(8, 16, 23, 23, ColShadow)
		box(10, 10, 24, 10, ColGreen)
		for xx := 18; xx < 23; xx += 2 {
			set(xx, 20, ColDark)
			set(xx, 22, ColDark)
		}
	case IcoFile:
		box(7, 3, 25, 28, ColWhite)
		frame(7, 3, 25, 28, ColDark)
		for i := 0; i < 5; i++ {
			set(25-i, 3+i, ColFace)
		}
		for i := 0; i < 4; i++ {
			set(25-i, 3+i, ColShadow)
		}
		for yy := 10; yy < 25; yy += 4 {
			for xx := 10; xx < 23; xx += 2 {
				set(xx, yy, ColShadow)
			}
		}
	case IcoText, IcoNotepad:
		box(7, 3, 25, 28, ColWhite)
		frame(7, 3, 25, 28, ColDark)
		for i := 0; i < 4; i++ {
			set(25-i, 3+i, ColFace)
		}
		for yy := 10; yy < 26; yy += 3 {
			for xx := 10; xx < 22; xx++ {
				if xx%3 != 0 {
					set(xx, yy, ColShadow)
				}
			}
		}
	case IcoTerminal:
		box(3, 7, 29, 26, 0x101010)
		frame(3, 7, 29, 26, ColDark)
		for xx := 4; xx < 28; xx++ {
			set(xx, 8, ColWhite)
			set(xx, 8, ColShadow)
		}
		for xx := 4; xx < 28; xx++ {
			set(xx, 25, ColShadow)
		}
		for i := 0; i < 4; i++ {
			set(7+i, 14, ColGreen)
			set(7+i, 15, ColGreen)
			set(11+i, 19+i, ColGreen)
			set(11+i, 20+i, ColGreen)
		}
		set(8, 13, ColGreen)
	case IcoBrowser:
		for yy := 4; yy < 29; yy++ {
			for xx := 4; xx < 29; xx++ {
				dx, dy := xx-16, yy-16
				d := dx*dx + dy*dy
				if d <= 132 && d >= 80 {
					set(xx, yy, 0x2060C0)
				}
			}
		}
		for yy := 8; yy < 24; yy++ {
			set(16, yy, 0x40A0E0)
		}
		for xx := 8; xx < 24; xx++ {
			set(xx, 16, 0x40A0E0)
			set(xx, 11+(xx-8)/2, 0x40A0E0)
			set(xx, 21-(xx-8)/2, 0x40A0E0)
		}
		frame(4, 16, 28, 16, 0x104080)
	case IcoCalc:
		box(6, 3, 25, 29, ColFace)
		frame(6, 3, 25, 29, ColDark)
		box(8, 6, 23, 11, 0xC8E0C0)
		frame(8, 6, 23, 11, ColShadow)
		for yy := 14; yy < 28; yy += 4 {
			for xx := 8; xx < 24; xx += 5 {
				box(xx, yy, xx+3, yy+2, ColLight)
				set(xx, yy, ColWhite)
				set(xx+3, yy+2, ColShadow)
			}
		}
	case IcoCalendar:
		box(4, 6, 27, 28, ColWhite)
		frame(4, 6, 27, 28, ColDark)
		for xx := 5; xx < 27; xx++ {
			for yy := 7; yy < 12; yy++ {
				set(xx, yy, ColRed)
			}
		}
		for xx := 5; xx < 27; xx++ {
			set(xx, 12, ColShadow)
		}
		for yy := 15; yy < 26; yy += 4 {
			for xx := 8; xx < 25; xx += 4 {
				set(xx, yy, 0x2060C0)
				set(xx+1, yy+1, 0x2060C0)
			}
		}
		for _, xx := range []int{8, 22} {
			box(xx, 3, xx+2, 8, ColLight)
			frame(xx, 3, xx+2, 8, ColDark)
		}
	case IcoNetwork:
		box(3, 9, 12, 17, ColFace)
		frame(3, 9, 12, 17, ColDark)
		box(19, 9, 28, 17, ColFace)
		frame(19, 9, 28, 17, ColDark)
		box(11, 21, 20, 27, ColFace)
		frame(11, 21, 20, 27, ColDark)
		for xx := 12; xx < 20; xx++ {
			set(xx, 18, ColShadow)
		}
		for yy := 17; yy < 22; yy++ {
			set(8, yy, ColShadow)
			set(23, yy, ColShadow)
		}
		set(15, 24, ColGreen)
		set(16, 24, ColGreen)
		set(15, 25, ColGreen)
		set(16, 25, ColGreen)
	case IcoInfo:
		box(4, 4, 27, 27, ColWhite)
		frame(4, 4, 27, 27, ColDark)
		box(14, 9, 17, 12, ColNavy)
		box(14, 15, 17, 23, ColNavy)
		for xx := 8; xx < 24; xx++ {
			set(xx, 6, 0x80C0E0)
		}
	case IcoDownload:
		box(8, 4, 24, 25, ColWhite)
		frame(8, 4, 24, 25, ColDark)
		for xx := 10; xx < 23; xx++ {
			set(xx, 7, ColShadow)
		}
		for yy := 11; yy < 20; yy++ {
			for xx := 14; xx < 18; xx++ {
				set(xx, yy, ColGreen)
			}
		}
		for i := 0; i < 5; i++ {
			for xx := 11 + i; xx < 21-i; xx++ {
				set(xx, 19+i, ColGreen)
			}
		}
	case IcoSetup:
		box(5, 5, 26, 27, ColFace)
		frame(5, 5, 26, 27, ColDark)
		box(8, 8, 23, 15, ColShadow)
		frame(8, 8, 23, 15, ColDark)
		for yy := 9; yy < 15; yy++ {
			for xx := 9; xx < 23; xx++ {
				set(xx, yy, 0xA0A0A0)
			}
		}
		box(18, 10, 22, 13, 0x202020)
		box(8, 19, 23, 24, ColLight)
		frame(8, 19, 23, 24, ColShadow)
	case IcoPackage:
		box(4, 9, 27, 28, 0xD0A860)
		frame(4, 9, 27, 28, 0x604020)
		box(4, 5, 27, 9, 0xE8C888)
		frame(4, 5, 27, 9, 0x604020)
		for yy := 9; yy < 28; yy++ {
			set(15, yy, 0xF0DCA8)
			set(16, yy, 0xC09050)
		}
		box(7, 12, 12, 18, ColWhite)
		frame(7, 12, 12, 18, ColShadow)
		for yy := 14; yy < 17; yy += 2 {
			for xx := 8; xx < 12; xx += 2 {
				set(xx, yy, ColShadow)
			}
		}
		box(19, 13, 24, 24, 0xF8F0D8)
		frame(19, 13, 24, 24, ColShadow)
		for yy := 16; yy < 23; yy += 2 {
			set(21, yy, ColShadow)
		}
	case IcoMine:
		for yy := 3; yy < 29; yy++ {
			for xx := 3; xx < 29; xx++ {
				dx, dy := xx-16, yy-17
				if dx*dx+dy*dy <= 52 {
					set(xx, yy, ColDark)
				}
			}
		}
		for yy := 6; yy < 28; yy += 2 {
			set(16, yy, ColDark)
		}
		for xx := 5; xx < 27; xx += 2 {
			set(xx, 17, ColDark)
		}
		for _, p := range [][2]int{{10, 9}, {22, 9}, {10, 25}, {22, 25}} {
			set(p[0], p[1], ColDark)
			set(p[0]+1, p[1], ColDark)
		}
		box(12, 13, 13, 13, ColWhite)
		box(12, 14, 12, 14, ColWhite)
	case IcoPaint:
		for yy := 4; yy < 28; yy++ {
			for xx := 2; xx < 22; xx++ {
				dx, dy := xx-12, yy-16
				if dx*dx+dy*dy <= 110 {
					set(xx, yy, 0xE8C880)
				}
			}
		}
		for yy := 7; yy < 12; yy++ {
			for xx := 3; xx < 9; xx++ {
				dx, dy := xx-6, yy-9
				if dx*dx+dy*dy <= 6 {
					set(xx, yy, ColWhite)
				}
			}
		}
		colDots := []uint32{ColRed, 0x2060C0, ColGreen, 0xF0C000}
		pos := [][2]int{{14, 9}, {18, 12}, {17, 18}, {12, 22}}
		for i, p := range pos {
			for dy := 0; dy < 3; dy++ {
				for dx := 0; dx < 3; dx++ {
					set(p[0]+dx, p[1]+dy, colDots[i])
				}
			}
		}
		for i := 0; i < 12; i++ {
			set(23+i, 26-i, 0x805020)
			set(24+i, 26-i, 0xC09050)
			if i > 8 {
				set(23+i, 26-i, ColShadow)
			}
		}
		for dy := 0; dy < 3; dy++ {
			for dx := 0; dx < 3; dx++ {
				set(22+dx, 26+dy-1, ColDark)
			}
		}
	case IcoNuke:
		for yy := 2; yy < 30; yy++ {
			for xx := 2; xx < 30; xx++ {
				dx, dy := xx-16, yy-16
				if dx*dx+dy*dy <= 180 {
					set(xx, yy, 0xC00000)
				}
			}
		}
		for _, p := range [][2]int{{16, 10}, {11, 21}, {21, 21}} {
			for yy := -4; yy <= 4; yy++ {
				for xx := -4; xx <= 4; xx++ {
					if xx*xx+yy*yy <= 17 {
						set(p[0]+xx, p[1]+yy, ColDark)
					}
				}
			}
		}
		for yy := -3; yy <= 3; yy++ {
			for xx := -3; xx <= 3; xx++ {
				if xx*xx+yy*yy <= 9 {
					set(16+xx, 16+yy, 0xFFE000)
				}
			}
		}
	case IcoCards:
		box(5, 7, 21, 29, ColFace)
		frame(5, 7, 21, 29, ColDark)
		for yy := 9; yy < 28; yy += 3 {
			for xx := 7; xx < 20; xx += 2 {
				set(xx, yy, ColShadow)
			}
		}
		box(11, 3, 28, 25, ColWhite)
		frame(11, 3, 28, 25, ColDark)
		for yy := -5; yy <= 5; yy++ {
			for xx := -5; xx <= 5; xx++ {
				if (xx < 0 && -xx+yy <= 5) || (xx >= 0 && xx+yy <= 5) ||
					(xx < 0 && -xx-yy <= 5) || (xx >= 0 && xx-yy <= 5) {
					set(19+xx, 14+yy, ColRed)
				}
			}
		}
	}
}

// DrawIcon16 draws a compact 16x16 icon used in title bars, taskbar and lists.
func DrawIcon16(c *Canvas, kind IconKind, x, y int) {
	set := func(xx, yy int, col uint32) { c.Set(x+xx, y+yy, col) }
	box := func(x0, y0, x1, y1 int, col uint32) {
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				set(xx, yy, col)
			}
		}
	}
	frame := func(x0, y0, x1, y1 int, col uint32) {
		for xx := x0; xx <= x1; xx++ {
			set(xx, y0, col)
			set(xx, y1, col)
		}
		for yy := y0; yy <= y1; yy++ {
			set(x0, yy, col)
			set(x1, yy, col)
		}
	}
	switch kind {
	case IcoComputer:
		box(1, 2, 14, 10, ColLight)
		frame(1, 2, 14, 10, ColDark)
		box(3, 4, 12, 8, ColDesktop)
		box(6, 11, 9, 13, ColShadow)
		box(3, 14, 12, 14, ColShadow)
	case IcoFolder, IcoFolderOpen:
		box(1, 5, 14, 13, 0xFFD040)
		box(1, 3, 7, 5, 0xFFD040)
		frame(1, 3, 14, 13, 0x806000)
		for xx := 1; xx < 15; xx++ {
			set(xx, 5, 0xFFF0A0)
		}
	case IcoFile, IcoText, IcoNotepad:
		box(3, 1, 12, 14, ColWhite)
		frame(3, 1, 12, 14, ColDark)
		box(9, 1, 12, 4, ColFace)
		frame(9, 1, 12, 4, ColShadow)
		for yy := 6; yy < 13; yy += 2 {
			for xx := 5; xx < 11; xx++ {
				if xx%2 == 0 {
					set(xx, yy, ColShadow)
				}
			}
		}
	case IcoTerminal:
		box(1, 2, 14, 13, 0x101010)
		frame(1, 2, 14, 13, ColDark)
		box(3, 5, 5, 5, ColGreen)
		set(6, 6, ColGreen)
		set(5, 7, ColGreen)
		box(9, 7, 12, 7, ColLight)
		box(3, 9, 11, 9, ColLight)
	case IcoBrowser:
		for yy := 2; yy < 14; yy++ {
			for xx := 2; xx < 14; xx++ {
				dx, dy := xx-8, yy-8
				d := dx*dx + dy*dy
				if d <= 36 && d >= 22 {
					set(xx, yy, 0x2060C0)
				}
			}
		}
		for yy := 4; yy < 12; yy++ {
			set(8, yy, 0x40A0E0)
		}
		for xx := 4; xx < 13; xx++ {
			set(xx, 8, 0x40A0E0)
		}
	case IcoCalc:
		box(2, 1, 13, 14, ColFace)
		frame(2, 1, 13, 14, ColDark)
		box(4, 3, 11, 6, 0xC8E0C0)
		frame(4, 3, 11, 6, ColShadow)
		for yy := 8; yy < 14; yy += 2 {
			for xx := 4; xx < 12; xx += 2 {
				set(xx, yy, ColLight)
				set(xx+1, yy+1, ColShadow)
			}
		}
	case IcoCalendar:
		box(1, 3, 14, 14, ColWhite)
		frame(1, 3, 14, 14, ColDark)
		box(2, 4, 13, 6, ColRed)
		for yy := 8; yy < 13; yy += 2 {
			for xx := 3; xx < 13; xx += 3 {
				set(xx, yy, 0x2060C0)
			}
		}
	case IcoDisk, IcoDrive:
		box(2, 3, 13, 13, ColFace)
		frame(2, 3, 13, 13, ColDark)
		box(3, 4, 12, 6, ColShadow)
		box(4, 9, 11, 12, ColWhite)
		frame(4, 9, 11, 12, ColShadow)
		set(11, 5, ColGreen)
	case IcoRecycle:
		box(4, 4, 11, 13, ColLight)
		frame(4, 4, 11, 13, ColShadow)
		box(3, 2, 12, 3, ColShadow)
		for yy := 6; yy < 12; yy += 3 {
			set(6, yy, ColShadow)
			set(9, yy, ColShadow)
		}
	case IcoNetwork:
		box(1, 4, 6, 8, ColFace)
		frame(1, 4, 6, 8, ColDark)
		box(9, 4, 14, 8, ColFace)
		frame(9, 4, 14, 8, ColDark)
		box(5, 10, 10, 14, ColFace)
		frame(5, 10, 10, 14, ColDark)
		set(4, 9, ColShadow)
		set(11, 9, ColShadow)
		box(7, 12, 8, 12, ColGreen)
	case IcoInfo:
		box(2, 2, 13, 13, ColWhite)
		frame(2, 2, 13, 13, ColDark)
		box(7, 4, 8, 5, ColNavy)
		box(7, 7, 8, 11, ColNavy)
	case IcoDownload:
		box(4, 1, 12, 14, ColWhite)
		frame(4, 1, 12, 14, ColDark)
		box(7, 4, 8, 8, ColGreen)
		for i := 0; i < 3; i++ {
			box(5+i, 8+i, 10-i, 8+i, ColGreen)
		}
	case IcoPackage:
		box(1, 5, 14, 15, 0xD0A860)
		frame(1, 5, 14, 15, 0x604020)
		box(1, 2, 14, 5, 0xE8C888)
		frame(1, 2, 14, 5, 0x604020)
		for yy := 5; yy < 15; yy++ {
			set(8, yy, 0xF0DCA8)
		}
		box(4, 7, 6, 10, ColWhite)
		frame(4, 7, 6, 10, ColShadow)
		box(10, 7, 12, 12, 0xF8F0D8)
		frame(10, 7, 12, 12, ColShadow)
	case IcoMine:
		for yy := 2; yy < 15; yy++ {
			for xx := 2; xx < 15; xx++ {
				dx, dy := xx-8, yy-9
				if dx*dx+dy*dy <= 13 {
					set(xx, yy, ColDark)
				}
			}
		}
		for yy := 2; yy < 15; yy += 2 {
			set(8, yy, ColDark)
		}
		for xx := 2; xx < 15; xx += 2 {
			set(xx, 9, ColDark)
		}
		set(6, 7, ColWhite)
	case IcoPaint:
		for yy := 1; yy < 15; yy++ {
			for xx := 1; xx < 11; xx++ {
				dx, dy := xx-6, yy-8
				if dx*dx+dy*dy <= 30 {
					set(xx, yy, 0xE8C880)
				}
			}
		}
		set(7, 4, ColRed)
		set(8, 4, ColRed)
		set(9, 7, 0x2060C0)
		set(9, 8, 0x2060C0)
		set(6, 11, ColGreen)
		set(6, 12, ColGreen)
		box(12, 12, 14, 14, 0x805020)
	case IcoNuke:
		for yy := 1; yy < 15; yy++ {
			for xx := 1; xx < 15; xx++ {
				dx, dy := xx-8, yy-8
				if dx*dx+dy*dy <= 46 {
					set(xx, yy, 0xC00000)
				}
			}
		}
		for _, p := range [][2]int{{8, 4}, {5, 11}, {11, 11}} {
			for yy := -2; yy <= 2; yy++ {
				for xx := -2; xx <= 2; xx++ {
					if xx*xx+yy*yy <= 5 {
						set(p[0]+xx, p[1]+yy, ColDark)
					}
				}
			}
		}
		for yy := -1; yy <= 1; yy++ {
			for xx := -1; xx <= 1; xx++ {
				set(8+xx, 8+yy, 0xFFE000)
			}
		}
	case IcoCards:
		box(2, 4, 10, 14, ColFace)
		frame(2, 4, 10, 14, ColDark)
		box(5, 1, 13, 11, ColWhite)
		frame(5, 1, 13, 11, ColDark)
		set(9, 5, ColRed)
		set(8, 6, ColRed)
		set(9, 6, ColRed)
		set(10, 6, ColRed)
		set(9, 7, ColRed)
	default:
		box(3, 3, 12, 12, ColFace)
		frame(3, 3, 12, 12, ColDark)
	}
}

// CursorHand draws a hand pointer for hyperlinks.
func CursorHand(c *Canvas, x, y int) {
	rows := []string{
		"....XX.......",
		"...X##X......",
		"...X##X......",
		"...X##X......",
		"...X##XX.....",
		"...X##X#XX...",
		"XX.X##X##X...",
		"X#XX#####X...",
		".X##X#####X..",
		"..X#######X..",
		"...X######X..",
		"....X#####X..",
		".....XXXXX...",
	}
	for yy, s := range rows {
		for xx, ch := range s {
			switch ch {
			case 'X':
				c.Set(x+xx, y+yy, ColDark)
			case '#':
				c.Set(x+xx, y+yy, ColWhite)
			}
		}
	}
}
