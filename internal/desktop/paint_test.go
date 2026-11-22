package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTestPaint(t *testing.T, width, height int) (*Desktop, *PaintApp) {
	t.Helper()
	d := New(width, height, ModeDesktop, Host{})
	d.openPaint()
	for _, w := range d.windows {
		if a, ok := w.App.(*PaintApp); ok {
			if a.win == nil {
				t.Fatal("paint window was not linked to the app")
			}
			return d, a
		}
	}
	t.Fatal("paint did not open")
	return nil, nil
}

func writeTestPNG(t *testing.T, w, h int, at image.Point, col color.RGBA) string {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	src.SetRGBA(at.X, at.Y, col)
	path := filepath.Join(t.TempDir(), "示例.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPaintOpensPNGAndCenters(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	path := writeTestPNG(t, 40, 30, image.Pt(10, 20), color.RGBA{12, 34, 56, 255})
	a.openPath(d, path)
	if a.cw < 40 || a.ch < 30 {
		t.Fatalf("canvas = %d×%d, want at least 40×30", a.cw, a.ch)
	}
	ox, oy := (a.cw-40)/2, (a.ch-30)/2
	want := uint32(12)<<16 | 34<<8 | 56
	if got := a.pix[(oy+20)*a.cw+ox+10]; got != want {
		t.Fatalf("pixel = %06X, want %06X", got, want)
	}
	if !strings.Contains(a.status, "示例.png") {
		t.Fatalf("status = %q, want the file name", a.status)
	}
	if a.path != "" {
		t.Fatalf("opened image should not inherit a save path, got %q", a.path)
	}
}

func TestPaintOpenScalesOversizedImage(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	path := writeTestPNG(t, 3000, 2000, image.Pt(0, 0), color.RGBA{255, 0, 0, 255})
	a.openPath(d, path)
	if a.cw > d.W-20-18 {
		t.Fatalf("canvas width %d exceeds desktop", a.cw)
	}
	if a.ch > d.taskbarTop()-20-166 {
		t.Fatalf("canvas height %d exceeds desktop", a.ch)
	}
	if !strings.Contains(a.status, "已打开") {
		t.Fatalf("status = %q", a.status)
	}
}

func TestPaintOpenShowsFileDialog(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	a.open(d)
	if d.modal == nil {
		t.Fatal("Paint → 打开 should show the file dialog")
	}
}

func TestPaintRejectsUnsupportedOpen(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	path := filepath.Join(t.TempDir(), "动画.gif")
	if err := os.WriteFile(path, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openPath(d, path)
	if d.modal == nil {
		t.Fatal("unsupported image should raise an error dialog")
	}
}

func TestPaintSelectionAndMosaic(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	for y := 0; y < a.ch; y++ {
		for x := 0; x < a.cw; x++ {
			a.pix[y*a.cw+x] = uint32((x*7+y*13)>>4) << 16
		}
	}
	client := a.win.ClientRect()
	at := func(dx, dy int) MouseEvent { return MouseEvent{Button: 1, X: a.canvasR.X + dx, Y: a.canvasR.Y + dy} }
	a.tool = paintSelect
	down := at(20, 30)
	down.Kind = MouseDown
	a.Mouse(d, a.win, client, down)
	move := at(120, 130)
	move.Kind = MouseMove
	a.Mouse(d, a.win, client, move)
	up := at(120, 130)
	up.Kind = MouseUp
	a.Mouse(d, a.win, client, up)
	if !a.hasSel {
		t.Fatal("drag with the selection tool should create a selection")
	}
	if a.selX0 != 20 || a.selY0 != 30 || a.selX1 != 120 || a.selY1 != 130 {
		t.Fatalf("selection = (%d,%d)-(%d,%d)", a.selX0, a.selY0, a.selX1, a.selY1)
	}
	outside := a.pix[10*a.cw+10]
	a.tool = paintMosaic
	click := at(40, 40)
	click.Kind = MouseDown
	a.Mouse(d, a.win, client, click)
	click.Kind = MouseUp
	a.Mouse(d, a.win, client, click)
	want := a.pix[30*a.cw+20]
	for y := 30; y <= 38; y++ {
		for x := 20; x <= 28; x++ {
			if got := a.pix[y*a.cw+x]; got != want {
				t.Fatalf("mosaic block not uniform at %d,%d: %06X != %06X", x, y, got, want)
			}
		}
	}
	if a.pix[10*a.cw+10] != outside {
		t.Fatal("mosaic must not touch pixels outside the selection")
	}
	if !a.Key(d, a.win, KeyEvent{Name: "Esc"}) || a.hasSel {
		t.Fatal("Esc should clear the selection")
	}
}

func TestPaintSaveDefaultsToPNG(t *testing.T) {
	d, a := openTestPaint(t, 1024, 768)
	a.put(1, 1, 0x123456)
	path := filepath.Join(t.TempDir(), "作品")
	a.writeTo(d, path)
	if a.path != path+".png" {
		t.Fatalf("path = %q, want .png appended", a.path)
	}
	f, err := os.Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("saved file is not PNG: %v", err)
	}
	if got := color.RGBAModel.Convert(img.At(1, 1)).(color.RGBA); got.R != 0x12 || got.G != 0x34 || got.B != 0x56 {
		t.Fatalf("pixel = %v", got)
	}
}
