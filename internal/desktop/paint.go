package desktop

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"win95linux/internal/ui"
)

var paintTools = []string{"铅笔", "画刷", "橡皮", "直线", "矩形", "椭圆"}

var paintColors = []uint32{
	0x000000, 0x808080, 0x800000, 0x808000, 0x008000, 0x008080, 0x000080, 0x800080,
	0xFFFFFF, 0xC0C0C0, 0xFF0000, 0xFFFF00, 0x00FF00, 0x00FFFF, 0x0000FF, 0xFF00FF,
}

const (
	paintPencil = iota
	paintBrush
	paintEraser
	paintLine
	paintRect
	paintEllipse
)

const (
	paintToolbarH = 34
	// The palette needs two rows of swatches plus the status line below them;
	// 60px made the status text overlap the second swatch row.
	paintPaletteH = 80
)

type PaintApp struct {
	Base
	d   *Desktop
	win *Window

	cw, ch int
	pix    []uint32

	tool  int
	color uint32
	brush int

	dragging       bool
	startX, startY int
	lastX, lastY   int
	backup         []uint32

	canvasR    ui.Rect
	toolRects  []ui.Rect
	widthRects []ui.Rect
	colorRects []ui.Rect
	press      string
	status     string
	path       string
}

func NewPaintApp(d *Desktop) *PaintApp {
	return &PaintApp{d: d, color: 0x000000, brush: 3, status: "左键绘制；用“文件”菜单保存为 BMP"}
}

func (a *PaintApp) Title() string           { return "画图" }
func (a *PaintApp) Icon() ui.IconKind       { return ui.IcoPaint }
func (a *PaintApp) DefaultSize() (int, int) { return 760, 540 }
func (a *PaintApp) MinSize() (int, int)     { return 520, 380 }
func (a *PaintApp) Resizable() bool         { return true }

func (a *PaintApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "新建", Enabled: true, Action: func() { a.newImage(d) }},
			{Label: "保存", Accel: "Ctrl+S", Enabled: true, Action: func() { a.save(d, false) }},
			{Label: "另存为…", Enabled: true, Action: func() { a.save(d, true) }},
			{Separator: true},
			{Label: "退出", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "清除图像", Enabled: true, Action: func() { a.newImage(d) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于画图", Enabled: true, Action: func() {
				d.MessageBox("关于画图",
					"画图\n\n工具：铅笔、画刷、橡皮、直线、矩形、椭圆。\n下方调色板选择颜色，“笔宽”按钮调整粗细。\n\n图像以 24 位 BMP 格式保存在 /root/图片。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func paintCanvasRect(client ui.Rect) ui.Rect {
	r := ui.Rect{
		X: client.X + 6,
		Y: client.Y + paintToolbarH,
		W: client.W - 12,
		H: client.H - paintToolbarH - paintPaletteH,
	}
	if r.W < 32 {
		r.W = 32
	}
	if r.H < 32 {
		r.H = 32
	}
	return r
}

func (a *PaintApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	r := paintCanvasRect(client)
	if r.W == a.cw && r.H == a.ch && len(a.pix) > 0 {
		a.canvasR = r
		return
	}
	pix := make([]uint32, r.W*r.H)
	for i := range pix {
		pix[i] = 0xFFFFFF
	}
	mw, mh := a.cw, a.ch
	if mw > r.W {
		mw = r.W
	}
	if mh > r.H {
		mh = r.H
	}
	for y := 0; y < mh; y++ {
		copy(pix[y*r.W:y*r.W+mw], a.pix[y*a.cw:y*a.cw+mw])
	}
	a.pix, a.cw, a.ch = pix, r.W, r.H
	a.canvasR = r
}

func (a *PaintApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	a.toolRects = a.toolRects[:0]
	toolW := 56
	for i, name := range paintTools {
		br := ui.Rect{r.X + 6 + i*(toolW+3), r.Y + 5, toolW, 24}
		ui.Button(c, br, name, a.tool == i, true)
		a.toolRects = append(a.toolRects, br)
	}

	widths := []int{1, 3, 6}
	a.widthRects = a.widthRects[:0]
	totalW := len(widths)*26 + (len(widths)-1)*2
	x0 := r.Right() - 6 - totalW - 42
	f.Text(c, x0, r.Y+9, "笔宽:", ui.ColText)
	for i, wd := range widths {
		br := ui.Rect{x0 + 42 + i*28, r.Y + 5, 26, 24}
		ui.Button(c, br, strconv.Itoa(wd), a.brush == wd, true)
		a.widthRects = append(a.widthRects, br)
	}

	a.canvasR = paintCanvasRect(r)
	bevel := ui.Rect{a.canvasR.X - 2, a.canvasR.Y - 2, a.canvasR.W + 4, a.canvasR.H + 4}
	c.FillRect(bevel, ui.ColFace)
	c.Sunken(bevel)
	for y := 0; y < a.ch; y++ {
		dst := c.Pix[(a.canvasR.Y+y)*c.Stride+a.canvasR.X:]
		copy(dst[:a.cw], a.pix[y*a.cw:(y+1)*a.cw])
	}

	palY := r.Bottom() - paintPaletteH + 4
	a.colorRects = a.colorRects[:0]
	sw := 20
	for i, col := range paintColors {
		row, idx := i/8, i%8
		cr := ui.Rect{r.X + 6 + idx*(sw+2), palY + row*(sw+2), sw, sw}
		c.FillRect(cr, col)
		c.Frame(cr, ui.ColDark)
		if col == a.color {
			c.Frame(ui.Rect{cr.X - 1, cr.Y - 1, cr.W + 2, cr.H + 2}, ui.ColWhite)
			c.Frame(ui.Rect{cr.X - 2, cr.Y - 2, cr.W + 4, cr.H + 4}, ui.ColDark)
		}
		a.colorRects = append(a.colorRects, cr)
	}

	cur := ui.Rect{r.X + 6 + 8*(sw+2) + 10, palY, 46, 24}
	c.FillRect(cur, a.color)
	c.Frame(cur, ui.ColDark)
	f.Text(c, cur.Right()+8, cur.Y+4, "当前颜色", ui.ColText)

	f.TextClip(c, r.X+8, r.Bottom()-18, r.W-160, a.status, ui.ColText)
	size := fmt.Sprintf("%d × %d", a.cw, a.ch)
	f.TextRight(c, r.Right()-8, r.Bottom()-18, size, ui.ColText)
}

func (a *PaintApp) toCanvas(x, y int) (int, int) {
	cx, cy := x-a.canvasR.X, y-a.canvasR.Y
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx >= a.cw {
		cx = a.cw - 1
	}
	if cy >= a.ch {
		cy = a.ch - 1
	}
	return cx, cy
}

func (a *PaintApp) put(x, y int, col uint32) {
	if x < 0 || y < 0 || x >= a.cw || y >= a.ch {
		return
	}
	a.pix[y*a.cw+x] = col
}

func (a *PaintApp) dot(x, y, radius int, col uint32) {
	if radius < 1 {
		radius = 1
	}
	r := radius - 1
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r+1 {
				a.put(x+dx, y+dy, col)
			}
		}
	}
}

func (a *PaintApp) line(x0, y0, x1, y1 int, thickness int, col uint32) {
	dx := abs(x1 - x0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	dy := -abs(y1 - y0)
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		a.dot(x0, y0, thickness, col)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func (a *PaintApp) rect(x0, y0, x1, y1, thickness int, col uint32) {
	a.line(x0, y0, x1, y0, thickness, col)
	a.line(x0, y1, x1, y1, thickness, col)
	a.line(x0, y0, x0, y1, thickness, col)
	a.line(x1, y0, x1, y1, thickness, col)
}

func (a *PaintApp) ellipse(x0, y0, x1, y1, thickness int, col uint32) {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	rx, ry := abs(x1-x0)/2, abs(y1-y0)/2
	if rx < 2 || ry < 2 {
		a.line(x0, y0, x1, y1, thickness, col)
		return
	}
	steps := (rx + ry) * 3
	if steps < 32 {
		steps = 32
	}
	if steps > 900 {
		steps = 900
	}
	for i := 0; i < steps; i++ {
		t := 2 * math.Pi * float64(i) / float64(steps)
		x := cx + int(math.Round(float64(rx)*math.Cos(t)))
		y := cy + int(math.Round(float64(ry)*math.Sin(t)))
		a.dot(x, y, thickness, col)
	}
}

func (a *PaintApp) begin(x, y int) {
	cx, cy := a.toCanvas(x, y)
	a.dragging = true
	a.startX, a.startY = cx, cy
	a.lastX, a.lastY = cx, cy
	a.backup = append(a.backup[:0], a.pix...)
	switch a.tool {
	case paintPencil:
		a.dot(cx, cy, 1, a.color)
	case paintBrush:
		a.dot(cx, cy, a.brush, a.color)
	case paintEraser:
		a.dot(cx, cy, 9, 0xFFFFFF)
	}
}

func (a *PaintApp) drag(x, y int) {
	cx, cy := a.toCanvas(x, y)
	switch a.tool {
	case paintPencil:
		a.line(a.lastX, a.lastY, cx, cy, 1, a.color)
	case paintBrush:
		a.line(a.lastX, a.lastY, cx, cy, a.brush, a.color)
	case paintEraser:
		a.line(a.lastX, a.lastY, cx, cy, 9, 0xFFFFFF)
	case paintLine:
		copy(a.pix, a.backup)
		a.line(a.startX, a.startY, cx, cy, a.brush, a.color)
	case paintRect:
		copy(a.pix, a.backup)
		a.rect(a.startX, a.startY, cx, cy, a.brush, a.color)
	case paintEllipse:
		copy(a.pix, a.backup)
		a.ellipse(a.startX, a.startY, cx, cy, a.brush, a.color)
	}
	a.lastX, a.lastY = cx, cy
}

func (a *PaintApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseMove {
		if a.dragging {
			a.drag(ev.X, ev.Y)
			d.Invalidate()
		} else if a.canvasR.Contains(ev.X, ev.Y) {
			cx, cy := a.toCanvas(ev.X, ev.Y)
			a.status = fmt.Sprintf("坐标 %d, %d", cx, cy)
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		for i, r := range a.toolRects {
			if r.Contains(ev.X, ev.Y) {
				a.tool = i
				a.press = "tool"
				d.Invalidate()
				return
			}
		}
		widths := []int{1, 3, 6}
		for i, r := range a.widthRects {
			if r.Contains(ev.X, ev.Y) {
				a.brush = widths[i]
				a.press = "width"
				d.Invalidate()
				return
			}
		}
		for i, r := range a.colorRects {
			if r.Contains(ev.X, ev.Y) {
				a.color = paintColors[i]
				a.press = "color"
				d.Invalidate()
				return
			}
		}
		if a.canvasR.Contains(ev.X, ev.Y) {
			a.begin(ev.X, ev.Y)
			d.Invalidate()
		}
	case MouseUp:
		if a.dragging {
			a.dragging = false
		}
		a.press = ""
		d.Invalidate()
	}
}

func (a *PaintApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl && (ev.Rune == 's' || ev.Rune == 'S') {
		a.save(d, false)
		return true
	}
	return false
}

func (a *PaintApp) newImage(d *Desktop) {
	d.MessageBox("画图", "要清除当前图像并重新开始吗？", IconQuestion, []string{"确定", "取消"}, func(i int) {
		if i != 0 {
			return
		}
		for i := range a.pix {
			a.pix[i] = 0xFFFFFF
		}
		a.path = ""
		a.status = "已新建空白图像"
		d.Invalidate()
	})
}

func (a *PaintApp) save(d *Desktop, saveAs bool) {
	if a.path == "" || saveAs {
		name := "未命名.bmp"
		if a.path != "" {
			name = filepath.Base(a.path)
		}
		d.FileDialog(FileSave, "/root/图片", name, func(path string, ok bool) {
			if !ok || path == "" {
				return
			}
			a.writeTo(d, path)
		})
		return
	}
	a.writeTo(d, a.path)
}

func (a *PaintApp) writeTo(d *Desktop, path string) {
	if !strings.HasSuffix(strings.ToLower(path), ".bmp") {
		path += ".bmp"
	}
	if err := a.writeBMP(path); err != nil {
		d.MessageBox("画图", "保存失败: "+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a.path = path
	a.status = "已保存到 " + path
	d.Invalidate()
}

func (a *PaintApp) writeBMP(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	stride := (a.cw*3 + 3) &^ 3
	hdr := make([]byte, 54)
	copy(hdr[0:2], "BM")
	binary.LittleEndian.PutUint32(hdr[2:], uint32(54+stride*a.ch))
	binary.LittleEndian.PutUint32(hdr[10:], 54)
	binary.LittleEndian.PutUint32(hdr[14:], 40)
	binary.LittleEndian.PutUint32(hdr[18:], uint32(a.cw))
	binary.LittleEndian.PutUint32(hdr[22:], uint32(a.ch))
	binary.LittleEndian.PutUint16(hdr[26:], 1)
	binary.LittleEndian.PutUint16(hdr[28:], 24)
	binary.LittleEndian.PutUint32(hdr[34:], uint32(stride*a.ch))
	binary.LittleEndian.PutUint32(hdr[38:], 2835)
	binary.LittleEndian.PutUint32(hdr[42:], 2835)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(hdr); err != nil {
		return err
	}
	row := make([]byte, stride)
	for y := a.ch - 1; y >= 0; y-- {
		for x := 0; x < a.cw; x++ {
			v := a.pix[y*a.cw+x]
			row[x*3] = byte(v)
			row[x*3+1] = byte(v >> 8)
			row[x*3+2] = byte(v >> 16)
		}
		if _, err := f.Write(row); err != nil {
			return err
		}
	}
	return nil
}
