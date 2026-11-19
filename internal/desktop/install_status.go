package desktop

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const (
	installStatusPath = "/run/win95-install.status"
	installLogPath    = "/run/win95-install.log"
	installLogLines   = 12
)

// InstallStatusApp shows the progress published by win95-install-status.
// The installers run detached, so closing this window never stops them.
type InstallStatusApp struct {
	Base
	d        *Desktop
	win      *Window
	fallback string

	title string
	pct   int
	msg   string
	state string
	log   []string

	lastState string
	closeR    ui.Rect
	press     string
}

func NewInstallStatusApp(d *Desktop, title string) *InstallStatusApp {
	a := &InstallStatusApp{
		d:        d,
		fallback: title,
		msg:      "正在启动安装任务…",
		state:    "running",
		pct:      0,
	}
	a.refresh()
	return a
}

func (a *InstallStatusApp) Title() string {
	name := a.title
	if name == "" {
		name = a.fallback
	}
	if name == "" {
		name = "安装"
	}
	return name + " - 安装进度"
}

func (a *InstallStatusApp) Icon() ui.IconKind       { return ui.IcoPackage }
func (a *InstallStatusApp) DefaultSize() (int, int) { return 580, 380 }
func (a *InstallStatusApp) MinSize() (int, int)     { return 460, 280 }
func (a *InstallStatusApp) Resizable() bool         { return true }

func (a *InstallStatusApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if a.refresh() {
		d.Invalidate()
	}
}

func (a *InstallStatusApp) refresh() bool {
	changed := false
	if data, err := os.ReadFile(installStatusPath); err == nil {
		title, pct, msg, state := parseInstallStatus(string(data))
		if title != a.title || pct != a.pct || msg != a.msg || state != a.state {
			a.title, a.pct, a.msg, a.state = title, pct, msg, state
			changed = true
		}
	}
	if lines := tailTextLines(installLogPath, installLogLines); !equalStrings(lines, a.log) {
		a.log = lines
		changed = true
	}
	if a.lastState != a.state {
		if a.lastState != "" && (a.state == "done" || a.state == "failed") {
			beep()
		}
		a.lastState = a.state
		changed = true
	}
	return changed
}

// parseInstallStatus understands the key=value file written by
// win95-install-status. Unknown keys are ignored.
func parseInstallStatus(data string) (title string, pct int, msg, state string) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "title":
			title = value
		case "pct":
			if n, err := strconv.Atoi(value); err == nil {
				pct = n
			}
		case "msg":
			msg = value
		case "state":
			state = value
		}
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	switch state {
	case "running", "done", "failed":
	default:
		state = "running"
	}
	return title, pct, msg, state
}

func tailTextLines(path string, max int) []string {
	data, err := os.ReadFile(path)
	if err != nil || max <= 0 {
		return nil
	}
	raw := strings.Split(strings.ReplaceAll(string(data), "\r", "\n"), "\n")
	out := make([]string, 0, max)
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *InstallStatusApp) statusLine() (string, uint32) {
	switch a.state {
	case "done":
		return "安装完成", ui.ColGreen
	case "failed":
		return "安装失败", ui.ColRed
	case "running":
		return "正在安装，请稍候…", ui.ColText
	default:
		return "", ui.ColText
	}
}

func (a *InstallStatusApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	name := a.title
	if name == "" {
		name = a.fallback
	}
	if name == "" {
		name = "安装"
	}
	f.TextBold(c, r.X+16, r.Y+14, name, ui.ColText)

	msg := a.msg
	if msg == "" {
		msg = "正在准备…"
	}
	f.TextClip(c, r.X+16, r.Y+40, r.W-32, msg, ui.ColText)

	bar := ui.Rect{r.X + 16, r.Y + 66, r.W - 32, 24}
	ui.ProgressBar(c, bar, a.pct)
	f.TextRight(c, r.Right()-16, r.Y+70, fmt.Sprintf("%d%%", a.pct), ui.ColText)

	line, col := a.statusLine()
	f.Text(c, r.X+16, r.Y+98, line, col)
	f.TextRight(c, r.Right()-16, r.Y+98, "关闭窗口不会中断安装", ui.ColShadow)

	logR := ui.Rect{r.X + 16, r.Y + 120, r.W - 32, r.H - 120 - 50}
	if logR.H > 16 {
		c.FillRect(logR, ui.ColWhite)
		c.Sunken(logR)
		lineH := 16
		maxLines := (logR.H - 8) / lineH
		if maxLines < 0 {
			maxLines = 0
		}
		lines := a.log
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
		for i, text := range lines {
			lineCol := ui.ColText
			if strings.HasPrefix(text, "失败") || strings.Contains(strings.ToLower(text), "error") {
				lineCol = ui.ColRed
			}
			f.TextClip(c, logR.X+5, logR.Y+4+i*lineH, logR.W-10, text, lineCol)
		}
		if len(lines) == 0 {
			f.Text(c, logR.X+5, logR.Y+4, "等待安装程序输出…", ui.ColTextDis)
		}
	}

	a.closeR = ui.Rect{r.Right() - 16 - 88, r.Bottom() - 36, 88, 26}
	ui.Button(c, a.closeR, "关闭", a.press == "close", true)
}

func (a *InstallStatusApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.closeR.Contains(ev.X, ev.Y) {
			a.press = "close"
			d.Invalidate()
		}
	case MouseUp:
		press := a.press
		a.press = ""
		if press == "close" && a.closeR.Contains(ev.X, ev.Y) {
			d.CloseWindow(w)
			return
		}
		d.Invalidate()
	}
}

func (a *InstallStatusApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" {
		d.CloseWindow(w)
		return true
	}
	return false
}

// openInstallStatus reuses an existing progress window instead of stacking
// several copies for a second install.
func (d *Desktop) openInstallStatus(title string) *InstallStatusApp {
	for _, w := range d.windows {
		if app, ok := w.App.(*InstallStatusApp); ok {
			d.bringToFront(w)
			app.refresh()
			d.Invalidate()
			return app
		}
	}
	app := NewInstallStatusApp(d, title)
	w := d.Open(app)
	app.win = w
	return app
}
