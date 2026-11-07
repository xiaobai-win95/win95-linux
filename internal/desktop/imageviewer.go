package desktop

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

// ImageViewerApp displays PNG and BMP images with fit-to-window, zoom and
// drag panning.
type ImageViewerApp struct {
	Base
	d   *Desktop
	win *Window

	path string
	img  *image.RGBA
	iw   int
	ih   int
	size int64
	err  string

	fit  bool
	zoom float64
	panX int
	panY int

	drag     bool
	dragX    int
	dragY    int
	dragPanX int
	dragPanY int

	lastClick time.Time
	status    string

	btnOpen, btnIn, btnOut, btnFit, btnOne ui.Rect
	area, statusR                          ui.Rect
	press                                  string
}

func NewImageViewerApp(d *Desktop) *ImageViewerApp {
	return &ImageViewerApp{d: d, fit: true, zoom: 1}
}

func (a *ImageViewerApp) Title() string {
	if a.path == "" {
		return "图片查看器"
	}
	return filepath.Base(a.path) + " - 图片查看器"
}

func (a *ImageViewerApp) Icon() ui.IconKind       { return ui.IcoImage }
func (a *ImageViewerApp) DefaultSize() (int, int) { return 760, 560 }
func (a *ImageViewerApp) MinSize() (int, int)     { return 360, 260 }
func (a *ImageViewerApp) Resizable() bool         { return true }

func (a *ImageViewerApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "打开(O)…", Accel: "Ctrl+O", Enabled: true, Action: func() { a.openDialog(d) }},
			{Separator: true},
			{Label: "关闭(C)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "放大(I)", Accel: "+", Enabled: a.img != nil, Action: func() { a.zoomBy(2) }},
			{Label: "缩小(O)", Accel: "-", Enabled: a.img != nil, Action: func() { a.zoomBy(0.5) }},
			{Label: "适应窗口(F)", Accel: "F", Enabled: a.img != nil, Action: func() { a.setFit() }},
			{Label: "原始大小(1)", Accel: "1", Enabled: a.img != nil, Action: func() { a.setZoom(1) }},
			{Separator: true},
			{Label: "刷新(R)", Accel: "F5", Enabled: a.path != "", Action: func() { a.reload() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于图片查看器(A)", Enabled: true, Action: func() {
				d.MessageBox("关于图片查看器",
					"Windows 95 Linux 图片查看器\n版本 26.2\n\n支持 PNG（含透明通道）与未压缩 BMP。\n拖动图片可平移，Ctrl+滚轮可缩放。\n\n截屏（Print Screen）会保存为 BMP。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *ImageViewerApp) loadPath(path string) {
	a.path = path
	a.err = ""
	a.img = nil
	a.fit = true
	a.panX, a.panY = 0, 0
	data, err := os.ReadFile(path)
	if err != nil {
		a.err = "无法打开图片：" + err.Error()
		return
	}
	a.size = int64(len(data))
	img, err := decodeImageData(data, strings.ToLower(filepath.Ext(path)))
	if err != nil {
		a.err = err.Error()
		return
	}
	rgba := toRGBA(img)
	a.img = rgba
	a.iw = img.Bounds().Dx()
	a.ih = img.Bounds().Dy()
}

func (a *ImageViewerApp) reload() {
	if a.path != "" {
		a.loadPath(a.path)
		a.d.Invalidate()
	}
}

func (a *ImageViewerApp) openDialog(d *Desktop) {
	dir := "/root/图片"
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = "/root/下载"
	}
	d.FileDialog(FileOpen, dir, "", func(path string, ok bool) {
		if !ok || path == "" {
			return
		}
		a.loadPath(path)
		if a.win != nil {
			a.d.bringToFront(a.win)
		}
		d.Invalidate()
	})
}

func (a *ImageViewerApp) toolboxHeight() int { return 32 }

func (a *ImageViewerApp) layout(client ui.Rect) {
	a.statusR = ui.Rect{client.X, client.Bottom() - 20, client.W, 20}
	top := client.Y + a.toolboxHeight()
	h := client.H - a.toolboxHeight() - 20
	if h < 16 {
		h = 16
	}
	a.area = ui.Rect{client.X + 2, top + 2, client.W - 4, h - 4}
}

func (a *ImageViewerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, ui.ColFace)
	a.layout(client)
	f := ui.DefaultFont()

	tx := client.X + 4
	ty := client.Y + 4
	buttons := []struct {
		label string
		width int
		r     *ui.Rect
		id    string
	}{
		{"打开", 64, &a.btnOpen, "open"},
		{"放大", 64, &a.btnIn, "in"},
		{"缩小", 64, &a.btnOut, "out"},
		{"适应窗口", 82, &a.btnFit, "fit"},
		{"原始大小", 82, &a.btnOne, "one"},
	}
	for _, b := range buttons {
		*b.r = ui.Rect{tx, ty, b.width, 24}
		enabled := true
		if b.id != "open" {
			enabled = a.img != nil
		}
		ui.Button(c, *b.r, b.label, a.press == b.id, enabled)
		tx += b.width + 4
	}
	c.HLine(client.X+4, client.Y+31, client.W-8, ui.ColShadow)
	c.HLine(client.X+4, client.Y+32, client.W-8, ui.ColWhite)

	// Canvas with a subtle checkerboard for transparent pixels.
	c.FillRect(a.area, 0x606060)
	for y := a.area.Y; y < a.area.Bottom(); y += 8 {
		for x := a.area.X; x < a.area.Right(); x += 8 {
			if ((x-a.area.X)/8+(y-a.area.Y)/8)&1 == 0 {
				c.FillRect(ui.Rect{x, y, min(8, a.area.Right()-x), min(8, a.area.Bottom()-y)}, 0x686868)
			}
		}
	}

	if a.img == nil {
		msg := "打开 PNG 或 BMP 图片开始浏览。"
		if a.err != "" {
			msg = a.err
		}
		col := ui.ColWhite
		if a.err != "" {
			col = 0xFF8080
		}
		f.Text(c, a.area.X+(a.area.W-f.Width(msg))/2, a.area.Y+a.area.H/2-8, msg, col)
	} else {
		scale := a.scale()
		dstW := int(math.Round(float64(a.iw) * scale))
		dstH := int(math.Round(float64(a.ih) * scale))
		if dstW < 1 {
			dstW = 1
		}
		if dstH < 1 {
			dstH = 1
		}
		offX, offY := a.imageOrigin(dstW, dstH)
		x0, y0 := max(a.area.X, offX), max(a.area.Y, offY)
		x1 := min(a.area.Right(), offX+dstW)
		y1 := min(a.area.Bottom(), offY+dstH)
		for y := y0; y < y1; y++ {
			sy := (y - offY) * a.ih / dstH
			if sy < 0 || sy >= a.ih {
				continue
			}
			for x := x0; x < x1; x++ {
				sx := (x - offX) * a.iw / dstW
				if sx < 0 || sx >= a.iw {
					continue
				}
				o := sy*a.img.Stride + sx*4
				r := a.img.Pix[o]
				g := a.img.Pix[o+1]
				b := a.img.Pix[o+2]
				al := a.img.Pix[o+3]
				if al != 255 {
					bg := uint32(0x606060)
					if ((x-a.area.X)/8+(y-a.area.Y)/8)&1 == 1 {
						bg = 0x686868
					}
					r = uint8((uint32(r)*uint32(al) + ((bg>>16)&0xFF)*uint32(255-al)) / 255)
					g = uint8((uint32(g)*uint32(al) + ((bg>>8)&0xFF)*uint32(255-al)) / 255)
					b = uint8((uint32(b)*uint32(al) + (bg&0xFF)*uint32(255-al)) / 255)
				}
				c.Set(x, y, uint32(r)<<16|uint32(g)<<8|uint32(b))
			}
		}
		border := ui.Rect{offX - 1, offY - 1, dstW + 2, dstH + 2}
		c.Frame(border, ui.ColWhite)
	}

	// Status bar.
	st := a.statusR
	c.FillRect(st, ui.ColFace)
	c.HLine(st.X, st.Y, st.W, ui.ColShadow)
	c.HLine(st.X, st.Y+1, st.W, ui.ColWhite)
	status := a.status
	if status == "" {
		if a.img == nil {
			status = "就绪"
		} else {
			kind := strings.ToUpper(strings.TrimPrefix(filepath.Ext(a.path), "."))
			status = fmt.Sprintf("%d × %d · %s · %s · 缩放 %d%%", a.iw, a.ih, kind,
				formatBytes(a.size), int(math.Round(a.scale()*100)))
		}
	}
	f.Text(c, st.X+6, st.Y+3, status, ui.ColText)
	if a.img != nil {
		f.TextClip(c, st.X+st.W/2, st.Y+3, st.W/2-8, "拖动平移 · Ctrl+滚轮缩放", ui.ColShadow)
	}
}

func (a *ImageViewerApp) scale() float64 {
	if a.img == nil {
		return 1
	}
	if !a.fit && a.zoom > 0 {
		return a.zoom
	}
	sx := float64(a.area.W) / float64(a.iw)
	sy := float64(a.area.H) / float64(a.ih)
	s := math.Min(sx, sy)
	if s > 4 {
		s = 4
	}
	if s < 0.05 {
		s = 0.05
	}
	return s
}

func (a *ImageViewerApp) imageOrigin(dstW, dstH int) (int, int) {
	offX := a.area.X + (a.area.W-dstW)/2 + a.panX
	offY := a.area.Y + (a.area.H-dstH)/2 + a.panY
	if dstW <= a.area.W {
		offX = a.area.X + (a.area.W-dstW)/2
	} else {
		if offX > a.area.X {
			offX = a.area.X
		}
		if offX+dstW < a.area.Right() {
			offX = a.area.Right() - dstW
		}
	}
	if dstH <= a.area.H {
		offY = a.area.Y + (a.area.H-dstH)/2
	} else {
		if offY > a.area.Y {
			offY = a.area.Y
		}
		if offY+dstH < a.area.Bottom() {
			offY = a.area.Bottom() - dstH
		}
	}
	return offX, offY
}

func (a *ImageViewerApp) setFit() {
	a.fit = true
	a.panX, a.panY = 0, 0
	a.d.Invalidate()
}

func (a *ImageViewerApp) setZoom(z float64) {
	if a.img == nil {
		return
	}
	if z < 0.1 {
		z = 0.1
	}
	if z > 8 {
		z = 8
	}
	a.zoom = z
	a.fit = false
	a.panX, a.panY = 0, 0
	a.d.Invalidate()
}

func (a *ImageViewerApp) zoomBy(f float64) {
	if a.img == nil {
		return
	}
	cur := a.scale()
	a.setZoom(cur * f)
}

func (a *ImageViewerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	if ev.Kind == MouseWheel {
		if a.img == nil {
			return
		}
		if ev.Ctrl {
			if ev.Wheel > 0 {
				a.zoomBy(1.25)
			} else {
				a.zoomBy(0.8)
			}
			return
		}
		scale := a.scale()
		dstW := int(float64(a.iw) * scale)
		dstH := int(float64(a.ih) * scale)
		if dstH <= a.area.H && dstW <= a.area.W {
			return
		}
		if ev.Shift {
			a.panX += ev.Wheel * 48
		} else {
			a.panY += ev.Wheel * 48
		}
		d.Invalidate()
		return
	}
	if ev.Kind == MouseMove && a.drag {
		a.panX = a.dragPanX + (ev.X - a.dragX)
		a.panY = a.dragPanY + (ev.Y - a.dragY)
		d.Invalidate()
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnIn.Contains(ev.X, ev.Y):
			a.press = "in"
		case a.btnOut.Contains(ev.X, ev.Y):
			a.press = "out"
		case a.btnFit.Contains(ev.X, ev.Y):
			a.press = "fit"
		case a.btnOne.Contains(ev.X, ev.Y):
			a.press = "one"
		case a.area.Contains(ev.X, ev.Y):
			now := time.Now()
			if now.Sub(a.lastClick) < 400*time.Millisecond {
				if a.fit {
					a.setZoom(1)
				} else {
					a.setFit()
				}
				a.lastClick = time.Time{}
				return
			}
			a.lastClick = now
			a.drag = true
			a.dragX, a.dragY = ev.X, ev.Y
			a.dragPanX, a.dragPanY = a.panX, a.panY
		}
	case MouseUp:
		p := a.press
		a.press = ""
		a.drag = false
		switch p {
		case "open":
			if a.btnOpen.Contains(ev.X, ev.Y) {
				a.openDialog(d)
			}
		case "in":
			if a.btnIn.Contains(ev.X, ev.Y) {
				a.zoomBy(2)
			}
		case "out":
			if a.btnOut.Contains(ev.X, ev.Y) {
				a.zoomBy(0.5)
			}
		case "fit":
			if a.btnFit.Contains(ev.X, ev.Y) {
				a.setFit()
			}
		case "one":
			if a.btnOne.Contains(ev.X, ev.Y) {
				a.setZoom(1)
			}
		}
		d.Invalidate()
	}
}

func (a *ImageViewerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl && strings.EqualFold(ev.Name, "O") {
		a.openDialog(d)
		return true
	}
	switch ev.Name {
	case "+", "=":
		a.zoomBy(2)
		return true
	case "-":
		a.zoomBy(0.5)
		return true
	case "F":
		a.setFit()
		return true
	case "1":
		a.setZoom(1)
		return true
	case "F5":
		a.reload()
		return true
	case "Left":
		a.panX += 24
		a.d.Invalidate()
		return true
	case "Right":
		a.panX -= 24
		a.d.Invalidate()
		return true
	case "Up":
		a.panY += 24
		a.d.Invalidate()
		return true
	case "Down":
		a.panY -= 24
		a.d.Invalidate()
		return true
	case "PgUp":
		a.panY += a.area.H
		a.d.Invalidate()
		return true
	case "PgDn":
		a.panY -= a.area.H
		a.d.Invalidate()
		return true
	}
	return false
}

func (a *ImageViewerApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	a.panX, a.panY = 0, 0
}

// --- Decoders ---------------------------------------------------------------

func decodeImageData(data []byte, ext string) (image.Image, error) {
	switch ext {
	case ".png":
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("PNG 文件已损坏或格式不受支持：%v", err)
		}
		return img, nil
	case ".bmp":
		return decodeBMP(data)
	}
	return nil, fmt.Errorf("不支持的图片格式“%s”，仅支持 PNG 与 BMP。", ext)
}

func toRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func decodeBMP(data []byte) (image.Image, error) {
	if len(data) < 54 || data[0] != 'B' || data[1] != 'M' {
		return nil, fmt.Errorf("BMP 文件头无效。")
	}
	off := int(binary.LittleEndian.Uint32(data[10:14]))
	dib := int(binary.LittleEndian.Uint32(data[14:18]))
	if dib < 40 {
		return nil, fmt.Errorf("不支持此 BMP 变体。")
	}
	w := int(int32(binary.LittleEndian.Uint32(data[18:22])))
	hRaw := int(int32(binary.LittleEndian.Uint32(data[22:26])))
	bpp := int(binary.LittleEndian.Uint16(data[28:30]))
	comp := binary.LittleEndian.Uint32(data[30:34])
	if w <= 0 || w > 8192 || hRaw == 0 || hRaw < -8192 || hRaw > 8192 {
		return nil, fmt.Errorf("BMP 尺寸不受支持。")
	}
	if comp != 0 && comp != 3 {
		return nil, fmt.Errorf("仅支持未压缩的 BMP 文件。")
	}
	if bpp != 24 && bpp != 32 {
		return nil, fmt.Errorf("仅支持 24 位或 32 位 BMP。")
	}
	topDown := hRaw < 0
	h := hRaw
	if h < 0 {
		h = -h
	}
	bytesPerPx := bpp / 8
	stride := (w*bytesPerPx + 3) &^ 3
	if off < 54 || off+stride*h > len(data) {
		return nil, fmt.Errorf("BMP 像素数据不完整。")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := y
		if !topDown {
			sy = h - 1 - y
		}
		row := data[off+sy*stride:]
		for x := 0; x < w; x++ {
			b := row[x*bytesPerPx]
			g := row[x*bytesPerPx+1]
			r := row[x*bytesPerPx+2]
			a := uint8(255)
			if bytesPerPx == 4 {
				a = row[x*4+3]
				if a == 0 {
					a = 255
				}
			}
			o := y*img.Stride + x*4
			img.Pix[o] = r
			img.Pix[o+1] = g
			img.Pix[o+2] = b
			img.Pix[o+3] = a
		}
	}
	return img, nil
}
