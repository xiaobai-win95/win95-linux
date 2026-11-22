package desktop

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestScreenshotNotificationListsBothFormats(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.Host.Screenshot = func() (string, error) {
		return "/root/下载/截图-1.png\n/root/下载/截图-1.bmp", nil
	}
	d.takeScreenshot()
	if len(d.notices) != 1 {
		t.Fatalf("notices = %d, want 1", len(d.notices))
	}
	n := d.notices[0]
	if n.title != "截图已保存（PNG + BMP）" {
		t.Fatalf("title = %q", n.title)
	}
	if !strings.Contains(n.text, ".png") || !strings.Contains(n.text, ".bmp") {
		t.Fatalf("text = %q, want both paths", n.text)
	}
}

func TestPrintScreenOpensPaintWithPNG(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	path := writeTestPNG(t, 64, 48, image.Pt(32, 24), color.RGBA{1, 2, 3, 255})
	d.Host.Screenshot = func() (string, error) { return path + "\n" + path + ".bmp", nil }
	d.printScreenToPaint()

	var a *PaintApp
	for _, w := range d.windows {
		if p, ok := w.App.(*PaintApp); ok {
			a = p
		}
	}
	if a == nil {
		t.Fatal("Print Screen should open Paint")
	}
	if !strings.Contains(a.status, "已打开") {
		t.Fatalf("status = %q", a.status)
	}
	want := uint32(1)<<16 | 2<<8 | 3
	ox, oy := (a.cw-64)/2, (a.ch-48)/2
	if got := a.pix[(oy+24)*a.cw+ox+32]; got != want {
		t.Fatalf("captured pixel = %06X, want %06X", got, want)
	}
	if len(d.notices) == 0 {
		t.Fatal("screenshot path should be announced")
	}
}
