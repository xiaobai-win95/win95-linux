package desktop

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func testBMP(w, h int, rows [][]color.RGBA) []byte {
	stride := (w*3 + 3) &^ 3
	imageSize := stride * h
	buf := make([]byte, 54+imageSize)
	copy(buf, "BM")
	binary.LittleEndian.PutUint32(buf[2:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[10:], 54)
	binary.LittleEndian.PutUint32(buf[14:], 40)
	binary.LittleEndian.PutUint32(buf[18:], uint32(w))
	binary.LittleEndian.PutUint32(buf[22:], uint32(h))
	binary.LittleEndian.PutUint16(buf[26:], 1)
	binary.LittleEndian.PutUint16(buf[28:], 24)
	binary.LittleEndian.PutUint32(buf[34:], uint32(imageSize))
	for i := 0; i < h; i++ {
		row := rows[h-1-i]
		off := 54 + i*stride
		for x := 0; x < w; x++ {
			buf[off+x*3] = row[x].B
			buf[off+x*3+1] = row[x].G
			buf[off+x*3+2] = row[x].R
		}
	}
	return buf
}

func TestImageViewerLoadsPNG(t *testing.T) {
	dir := t.TempDir()
	src := image.NewRGBA(image.Rect(0, 0, 40, 30))
	src.SetRGBA(10, 20, color.RGBA{12, 34, 56, 255})
	path := filepath.Join(dir, "示例.png")
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
	v := NewImageViewerApp(nil)
	v.loadPath(path)
	if v.err != "" {
		t.Fatalf("loadPath: %s", v.err)
	}
	if v.iw != 40 || v.ih != 30 {
		t.Fatalf("size = %d×%d, want 40×30", v.iw, v.ih)
	}
	o := 20*v.img.Stride + 10*4
	if got := v.img.Pix[o : o+4]; got[0] != 12 || got[1] != 34 || got[2] != 56 || got[3] != 255 {
		t.Fatalf("pixel = %v, want [12 34 56 255]", got)
	}
	if v.Title() != "示例.png - 图片查看器" {
		t.Fatalf("title = %q", v.Title())
	}
}

func TestImageViewerLoadsBMPBottomUp(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	gray := color.RGBA{128, 128, 128, 255}
	rows := [][]color.RGBA{
		{white, black, gray},
		{red, green, blue},
	}
	path := filepath.Join(t.TempDir(), "截图.bmp")
	if err := os.WriteFile(path, testBMP(3, 2, rows), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewImageViewerApp(nil)
	v.loadPath(path)
	if v.err != "" {
		t.Fatalf("loadPath: %s", v.err)
	}
	if v.iw != 3 || v.ih != 2 {
		t.Fatalf("size = %d×%d, want 3×2", v.iw, v.ih)
	}
	check := func(x, y int, want color.RGBA) {
		t.Helper()
		o := y*v.img.Stride + x*4
		got := v.img.Pix[o : o+4]
		if got[0] != want.R || got[1] != want.G || got[2] != want.B || got[3] != 255 {
			t.Fatalf("pixel(%d,%d) = %v, want %v", x, y, got, want)
		}
	}
	check(0, 0, white)
	check(1, 0, black)
	check(2, 0, gray)
	check(0, 1, red)
	check(1, 1, green)
	check(2, 1, blue)
}

func TestImageViewerRejectsUnsupportedFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "动画.gif")
	if err := os.WriteFile(path, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewImageViewerApp(nil)
	v.loadPath(path)
	if v.err == "" {
		t.Fatal("expected an error for unsupported format")
	}
	if v.img != nil {
		t.Fatal("expected no image for unsupported format")
	}
}
