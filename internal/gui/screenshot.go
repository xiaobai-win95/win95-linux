package gui

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"win95linux/internal/ui"
)

// saveScreenshot encodes the last drawn framebuffer frame twice: a PNG for
// normal use and a 24-bit BMP for the vintage Paint workflow. Both files share
// one base name and their paths are returned as two lines.
func saveScreenshot(c *ui.Canvas) (string, error) {
	dir := "/root/下载"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := "截图-" + time.Now().Format("20060102-150405")
	var pngPath, bmpPath string
	for i := 1; i < 100; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		p := filepath.Join(dir, name+".png")
		b := filepath.Join(dir, name+".bmp")
		if !fileExists(p) && !fileExists(b) {
			pngPath, bmpPath = p, b
			break
		}
	}
	if pngPath == "" {
		return "", fmt.Errorf("截图文件名冲突过多")
	}
	if err := writePNGFile(pngPath, c); err != nil {
		_ = os.Remove(pngPath)
		return "", err
	}
	if err := writeBMPFile(bmpPath, c); err != nil {
		_ = os.Remove(pngPath)
		_ = os.Remove(bmpPath)
		return "", err
	}
	return pngPath + "\n" + bmpPath, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writePNGFile(path string, c *ui.Canvas) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for y := 0; y < c.H; y++ {
		src := c.Pix[y*c.Stride:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < c.W; x++ {
			v := src[x]
			o := x * 4
			dst[o] = byte(v >> 16)
			dst[o+1] = byte(v >> 8)
			dst[o+2] = byte(v)
			dst[o+3] = 0xFF
		}
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeBMPFile(path string, c *ui.Canvas) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := writeBMP(f, c); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

type bmpWriter interface {
	Write([]byte) (int, error)
}

func writeBMP(f bmpWriter, c *ui.Canvas) error {
	rowSize := (c.W*3 + 3) &^ 3
	imageSize := rowSize * c.H
	fileSize := 54 + imageSize
	hdr := make([]byte, 54)
	hdr[0], hdr[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(hdr[2:], uint32(fileSize))
	binary.LittleEndian.PutUint32(hdr[10:], 54)
	binary.LittleEndian.PutUint32(hdr[14:], 40)
	binary.LittleEndian.PutUint32(hdr[18:], uint32(c.W))
	binary.LittleEndian.PutUint32(hdr[22:], uint32(c.H))
	binary.LittleEndian.PutUint16(hdr[26:], 1)
	binary.LittleEndian.PutUint16(hdr[28:], 24)
	binary.LittleEndian.PutUint32(hdr[34:], uint32(imageSize))
	binary.LittleEndian.PutUint32(hdr[38:], 2835)
	binary.LittleEndian.PutUint32(hdr[42:], 2835)
	if _, err := f.Write(hdr); err != nil {
		return err
	}
	row := make([]byte, rowSize)
	for y := c.H - 1; y >= 0; y-- {
		src := c.Pix[y*c.Stride:]
		for x := 0; x < c.W; x++ {
			v := src[x]
			o := x * 3
			row[o] = byte(v)
			row[o+1] = byte(v >> 8)
			row[o+2] = byte(v >> 16)
		}
		if _, err := f.Write(row); err != nil {
			return err
		}
	}
	return nil
}
