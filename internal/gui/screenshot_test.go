package gui

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"win95linux/internal/ui"
)

func testCanvas() *ui.Canvas {
	c := ui.NewCanvas(2, 2)
	c.Pix[0*c.Stride+0] = 0x112233
	c.Pix[0*c.Stride+1] = 0x445566
	c.Pix[1*c.Stride+0] = 0x778899
	c.Pix[1*c.Stride+1] = 0xAABBCC
	return c
}

func TestWritePNGFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := writePNGFile(path, testCanvas()); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	check := func(x, y int, want [3]byte) {
		t.Helper()
		r, g, b, a := img.At(x, y).RGBA()
		if byte(r>>8) != want[0] || byte(g>>8) != want[1] || byte(b>>8) != want[2] || a != 0xFFFF {
			t.Fatalf("pixel(%d,%d) = %02X%02X%02X%02X", x, y, r>>8, g>>8, b>>8, a>>8)
		}
	}
	check(0, 0, [3]byte{0x11, 0x22, 0x33})
	check(1, 0, [3]byte{0x44, 0x55, 0x66})
	check(0, 1, [3]byte{0x77, 0x88, 0x99})
	check(1, 1, [3]byte{0xAA, 0xBB, 0xCC})
}

func TestWriteBMPBottomUp(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBMP(&buf, testCanvas()); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if len(b) != 54+16 {
		t.Fatalf("size = %d, want 70", len(b))
	}
	if string(b[:2]) != "BM" {
		t.Fatalf("magic = %q", b[:2])
	}
	if got := binary.LittleEndian.Uint32(b[18:]); got != 2 {
		t.Fatalf("width = %d", got)
	}
	if got := binary.LittleEndian.Uint32(b[22:]); got != 2 {
		t.Fatalf("height = %d", got)
	}
	// BMP rows are bottom-up and BGR; row padding is zero.
	want := []byte{
		0x99, 0x88, 0x77, 0xCC, 0xBB, 0xAA, 0, 0,
		0x33, 0x22, 0x11, 0x66, 0x55, 0x44, 0, 0,
	}
	if !bytes.Equal(b[54:], want) {
		t.Fatalf("pixels = % X, want % X", b[54:], want)
	}
}

func TestWriteBMPFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.bmp")
	if err := writeBMPFile(path, testCanvas()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 70 || b[0] != 'B' || b[1] != 'M' {
		t.Fatalf("bad BMP file, size %d", len(b))
	}
}
