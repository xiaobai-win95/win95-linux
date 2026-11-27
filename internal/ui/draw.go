package ui

// The classic palette is mutable so the settings app can switch themes at
// runtime; every draw call reads these values.
var (
	ColDesktop   uint32 = 0x008080
	ColFace      uint32 = 0xC0C0C0
	ColLight     uint32 = 0xDFDFDF
	ColWhite     uint32 = 0xFFFFFF
	ColShadow    uint32 = 0x808080
	ColDark      uint32 = 0x000000
	ColNavy      uint32 = 0x000080
	ColNavyLight uint32 = 0x1084D0
	ColTitleInac uint32 = 0x808080
	ColText      uint32 = 0x000000
	ColTextDis   uint32 = 0x808080
	ColField     uint32 = 0xFFFFFF
	ColYellow    uint32 = 0xFFFF00
	ColGreen     uint32 = 0x008000
	ColRed       uint32 = 0x800000
	ColBlue      uint32 = 0x0000A0
	ColBlack     uint32 = 0x000000
)

// Palette is one complete set of chrome colours.
type Palette struct {
	Name      string
	Desktop   uint32
	Face      uint32
	Light     uint32
	White     uint32
	Shadow    uint32
	Dark      uint32
	Navy      uint32
	NavyLight uint32
	TitleInac uint32
	Text      uint32
	TextDis   uint32
	Field     uint32
	Yellow    uint32
	Green     uint32
	Red       uint32
	Blue      uint32
}

var Palettes = []Palette{
	{
		Name: "经典 Windows 95", Desktop: 0x008080, Face: 0xC0C0C0, Light: 0xDFDFDF,
		White: 0xFFFFFF, Shadow: 0x808080, Dark: 0x000000, Navy: 0x000080,
		NavyLight: 0x1084D0, TitleInac: 0x808080, Text: 0x000000, TextDis: 0x808080,
		Field: 0xFFFFFF, Yellow: 0xFFFF00, Green: 0x008000, Red: 0x800000, Blue: 0x0000A0,
	},
	{
		Name: "午夜蓝", Desktop: 0x101C3A, Face: 0x2B3550, Light: 0x4C5A7A,
		White: 0xDCE4F5, Shadow: 0x111827, Dark: 0x000000, Navy: 0x1E3A8A,
		NavyLight: 0x3B82F6, TitleInac: 0x3C4660, Text: 0xE8EEF9, TextDis: 0x8A94AC,
		Field: 0x1C2438, Yellow: 0xFDE047, Green: 0x4ADE80, Red: 0xF87171, Blue: 0x93C5FD,
	},
	{
		Name: "高对比度", Desktop: 0x000000, Face: 0x000000, Light: 0xFFFFFF,
		White: 0xFFFFFF, Shadow: 0x808080, Dark: 0xFFFFFF, Navy: 0x000080,
		NavyLight: 0x00FFFF, TitleInac: 0x404040, Text: 0xFFFFFF, TextDis: 0xA0A0A0,
		Field: 0x000000, Yellow: 0xFFFF00, Green: 0x00FF00, Red: 0xFF0000, Blue: 0x00FFFF,
	},
	{
		Name: "青瓷", Desktop: 0x2F5D50, Face: 0xDDE8DC, Light: 0xF2F7EE,
		White: 0xFFFFFF, Shadow: 0x8FA393, Dark: 0x1C2B24, Navy: 0x2F5D50,
		NavyLight: 0x5E8C7A, TitleInac: 0x8FA393, Text: 0x1C2B24, TextDis: 0x7A8C80,
		Field: 0xFBFDF7, Yellow: 0xFFE066, Green: 0x3A7D44, Red: 0xA33B3B, Blue: 0x2B5F8A,
	},
	{
		Name: "玫瑰", Desktop: 0x6D2E46, Face: 0xF3E3E8, Light: 0xFDF3F6,
		White: 0xFFFFFF, Shadow: 0xB08A96, Dark: 0x2B141E, Navy: 0x8C3A5A,
		NavyLight: 0xC96B8C, TitleInac: 0xB08A96, Text: 0x2B141E, TextDis: 0x8C7680,
		Field: 0xFFF9FB, Yellow: 0xFFD166, Green: 0x4F7942, Red: 0xB13E53, Blue: 0x3D5A80,
	},
	{
		Name: "琥珀", Desktop: 0x3B2C16, Face: 0xE8DCC3, Light: 0xF7F0E0,
		White: 0xFFFFFF, Shadow: 0xA79A80, Dark: 0x241A0C, Navy: 0x7A5B1E,
		NavyLight: 0xC29A45, TitleInac: 0xA79A80, Text: 0x241A0C, TextDis: 0x867A64,
		Field: 0xFFFBEF, Yellow: 0xF2C14E, Green: 0x5B7B3A, Red: 0x9E3B2F, Blue: 0x3A6B8A,
	},
}

func ApplyPalette(p Palette) {
	ColDesktop = p.Desktop
	ColFace = p.Face
	ColLight = p.Light
	ColWhite = p.White
	ColShadow = p.Shadow
	ColDark = p.Dark
	ColNavy = p.Navy
	ColNavyLight = p.NavyLight
	ColTitleInac = p.TitleInac
	ColField = p.Field
	ColText = EnsureContrastPair(p.Text, p.Face, p.Field)
	ColTextDis = EnsureContrastPair(p.TextDis, p.Face, p.Field)
	ColYellow = p.Yellow
	ColGreen = p.Green
	ColRed = p.Red
	ColBlue = p.Blue
	ColBlack = 0x000000
}

type Rect struct {
	X, Y, W, H int
}

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.Right() && y < r.Bottom()
}

func (r Rect) Inset(n int) Rect {
	return Rect{r.X + n, r.Y + n, r.W - 2*n, r.H - 2*n}
}

// Canvas is a 32-bit XRGB surface. Pix holds 0x00RRGGBB values.
type Canvas struct {
	W, H, Stride int
	Pix          []uint32
}

func NewCanvas(w, h int) *Canvas {
	return &Canvas{W: w, H: h, Stride: w, Pix: make([]uint32, w*h)}
}

func (c *Canvas) Set(x, y int, col uint32) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	c.Pix[y*c.Stride+x] = col
}

func (c *Canvas) FillRect(r Rect, col uint32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	x0, y0 := r.X, r.Y
	x1, y1 := r.Right(), r.Bottom()
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.W {
		x1 = c.W
	}
	if y1 > c.H {
		y1 = c.H
	}
	for y := y0; y < y1; y++ {
		row := c.Pix[y*c.Stride:]
		for x := x0; x < x1; x++ {
			row[x] = col
		}
	}
}

func (c *Canvas) HLine(x, y, w int, col uint32) {
	c.FillRect(Rect{x, y, w, 1}, col)
}

func (c *Canvas) VLine(x, y, h int, col uint32) {
	c.FillRect(Rect{x, y, 1, h}, col)
}

func (c *Canvas) Frame(r Rect, col uint32) {
	c.HLine(r.X, r.Y, r.W, col)
	c.HLine(r.X, r.Bottom()-1, r.W, col)
	c.VLine(r.X, r.Y, r.H, col)
	c.VLine(r.Right()-1, r.Y, r.H, col)
}

// Raised draws a classic 2px raised 3D border.
func (c *Canvas) Raised(r Rect) {
	c.HLine(r.X, r.Y, r.W, ColWhite)
	c.VLine(r.X, r.Y, r.H, ColWhite)
	c.HLine(r.X, r.Bottom()-2, r.W, ColShadow)
	c.VLine(r.Right()-2, r.Y, r.H, ColShadow)
	c.HLine(r.X+1, r.Y+1, r.W-2, ColLight)
	c.VLine(r.X+1, r.Y+1, r.H-2, ColLight)
	c.HLine(r.X+1, r.Bottom()-1, r.W-1, ColDark)
	c.VLine(r.Right()-1, r.Y+1, r.H-1, ColDark)
}

// Sunken draws a classic 2px sunken 3D border.
func (c *Canvas) Sunken(r Rect) {
	c.HLine(r.X, r.Y, r.W, ColShadow)
	c.VLine(r.X, r.Y, r.H, ColShadow)
	c.HLine(r.X+1, r.Y+1, r.W-1, ColDark)
	c.VLine(r.X+1, r.Y+1, r.H-1, ColDark)
	c.HLine(r.X, r.Bottom()-1, r.W, ColWhite)
	c.VLine(r.Right()-1, r.Y, r.H, ColWhite)
	c.HLine(r.X+1, r.Bottom()-2, r.W-1, ColLight)
	c.VLine(r.Right()-2, r.Y+1, r.H-1, ColLight)
}

// ThinSunken draws a 1px sunken border.
func (c *Canvas) ThinSunken(r Rect) {
	c.HLine(r.X, r.Y, r.W, ColShadow)
	c.VLine(r.X, r.Y, r.H, ColShadow)
	c.HLine(r.X, r.Bottom()-1, r.W, ColWhite)
	c.VLine(r.Right()-1, r.Y, r.H, ColWhite)
}

// TitleBar fills a horizontal gradient from col1 to col2.
func (c *Canvas) TitleBar(r Rect, col1, col2 uint32) {
	if r.W <= 0 {
		return
	}
	for x := 0; x < r.W; x++ {
		t := float64(x) / float64(r.W)
		col := lerpColor(col1, col2, t)
		c.FillRect(Rect{r.X + x, r.Y, 1, r.H}, col)
	}
}

func lerpColor(a, b uint32, t float64) uint32 {
	ar, ag, ab := (a>>16)&0xff, (a>>8)&0xff, a&0xff
	br, bg, bb := (b>>16)&0xff, (b>>8)&0xff, b&0xff
	r := uint32(float64(ar) + (float64(br)-float64(ar))*t)
	g := uint32(float64(ag) + (float64(bg)-float64(ag))*t)
	bl := uint32(float64(ab) + (float64(bb)-float64(ab))*t)
	return r<<16 | g<<8 | bl
}

// Pattern fills the desktop with the classic 50% dither over teal.
func (c *Canvas) DesktopPattern(r Rect) {
	c.FillRect(r, ColDesktop)
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X + (y & 1); x < r.Right(); x += 2 {
			c.Set(x, y, 0x007F7F)
		}
	}
}

func (c *Canvas) Blit32(dst []uint32, dstStride int) {
	for y := 0; y < c.H; y++ {
		copy(dst[y*dstStride:y*dstStride+c.W], c.Pix[y*c.Stride:y*c.Stride+c.W])
	}
}
