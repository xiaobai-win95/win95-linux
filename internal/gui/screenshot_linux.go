//go:build linux

package gui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"win95linux/internal/ui"
)

// saveScreenshot encodes the last drawn framebuffer frame as a PNG under the
// user's download folder.
func saveScreenshot(c *ui.Canvas) (string, error) {
	dir := "/root/下载"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := "截图-" + time.Now().Format("20060102-150405")
	path := filepath.Join(dir, base+".png")
	for i := 2; i < 100; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, i))
	}
	img := image.NewNRGBA(image.Rect(0, 0, c.W, c.H))
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
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}
