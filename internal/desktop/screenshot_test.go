package desktop

import (
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
