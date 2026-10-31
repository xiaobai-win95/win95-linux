package desktop

import (
	"os"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const noticeLifetime = 8 * time.Second

// notifyQueuePath is written by XFCE helpers and other background jobs. Each
// line is "title<TAB>text"; the desktop drains the file and shows balloons.
var notifyQueuePath = "/run/win95-notify"

type notice struct {
	title string
	text  string
	at    time.Time
}

func (d *Desktop) visible() bool {
	return d.Host.Visible == nil || d.Host.Visible()
}

func (d *Desktop) Notify(title, text string) {
	if strings.TrimSpace(text) == "" {
		text = title
		title = "Windows 95 Linux"
	}
	d.notices = append(d.notices, notice{title: title, text: text, at: time.Now()})
	if d.noticeUntil.IsZero() {
		d.noticeUntil = time.Now().Add(noticeLifetime)
	}
	beep()
	d.Invalidate()
}

func (d *Desktop) pollNotifications() {
	if !d.visible() || notifyQueuePath == "" {
		return
	}
	tmp := notifyQueuePath + ".read"
	if err := os.Rename(notifyQueuePath, tmp); err != nil {
		return
	}
	b, err := os.ReadFile(tmp)
	_ = os.Remove(tmp)
	if err != nil {
		return
	}
	added := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		title, text, ok := strings.Cut(line, "\t")
		if !ok {
			title, text, ok = strings.Cut(line, "|")
		}
		if !ok {
			title, text = "通知", line
		}
		if strings.TrimSpace(title) == "" {
			title = "Windows 95 Linux"
		}
		d.notices = append(d.notices, notice{title: title, text: text, at: time.Now()})
		added = true
		if len(d.notices) > 8 {
			d.notices = d.notices[len(d.notices)-8:]
		}
	}
	if added {
		if d.noticeUntil.IsZero() {
			d.noticeUntil = time.Now().Add(noticeLifetime)
		}
		d.Invalidate()
	}
}

func (d *Desktop) dismissNotice() {
	if len(d.notices) == 0 {
		return
	}
	d.notices = d.notices[1:]
	if len(d.notices) > 0 {
		d.noticeUntil = time.Now().Add(noticeLifetime)
	} else {
		d.noticeUntil = time.Time{}
	}
	d.Invalidate()
}

func (d *Desktop) drawNotifications(c *ui.Canvas) {
	if len(d.notices) == 0 || !d.visible() {
		return
	}
	n := d.notices[0]
	f := ui.DefaultFont()
	const width = 380
	lines := wrapText(n.text, width-40)
	h := 42 + len(lines)*20
	if h < 74 {
		h = 74
	}
	x := d.W - width - 8
	y := d.taskbarTop() - h - 10
	if y < 8 {
		y = 8
	}
	r := ui.Rect{x, y, width, h}
	d.noticeRect = r
	c.FillRect(ui.Rect{r.X + 2, r.Y + 2, r.W, r.H}, ui.ColShadow)
	c.FillRect(r, 0xFFFFE1)
	c.Frame(r, ui.ColDark)
	f.TextBold(c, r.X+10, r.Y+8, n.title, ui.ColText)
	f.Text(c, r.Right()-20, r.Y+6, "×", ui.ColText)
	yy := r.Y + 30
	for _, line := range lines {
		f.TextClip(c, r.X+10, yy, r.W-20, line, ui.ColText)
		yy += 20
	}
	// A small tail points at the tray area.
	c.FillRect(ui.Rect{r.Right() - 34, r.Bottom(), 10, 2}, 0xFFFFE1)
	c.FillRect(ui.Rect{r.Right() - 33, r.Bottom() + 2, 8, 2}, 0xFFFFE1)
	c.FillRect(ui.Rect{r.Right() - 31, r.Bottom() + 4, 4, 2}, 0xFFFFE1)
}
