package desktop

import (
	"fmt"
	"os"
	"time"

	"win95linux/internal/ui"
)

const netscapeBrand = "Netscape Navigator 4.8"

var netscapeStatePath = "/root/.win95-netscape48"

func netscapeInstalled() bool {
	st, err := os.Stat(netscapeStatePath)
	return err == nil && !st.IsDir()
}

// openNetscape launches Netscape 4.8, asking once before the simulated
// installer places it on disk.
func (d *Desktop) openNetscape() {
	if netscapeInstalled() {
		d.openNetscapeBrowser("about:netscape")
		return
	}
	for _, w := range d.windows {
		if _, ok := w.App.(*netscapeInstallApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.MessageBox("Netscape Navigator 4.8",
		"Netscape Navigator 4.8 尚未安装。\n\n"+
			"该版本包含 Navigator 浏览器、邮件与新闻组客户端，\n"+
			"安装后可从桌面图标或“在线服务”启动。\n\n"+
			"是否现在安装？（约 2 MB，安装过程为离线模拟）",
		IconQuestion, []string{"安装", "以后再说"}, func(btn int) {
			if btn == 0 {
				d.beginNetscapeInstall()
			}
		})
}

func (d *Desktop) beginNetscapeInstall() {
	for _, w := range d.windows {
		if _, ok := w.App.(*netscapeInstallApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &netscapeInstallApp{d: d, started: time.Now()}
	a.win = d.OpenModal(a)
}

func (d *Desktop) openNetscapeBrowser(start string) {
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok && b.brand == netscapeBrand {
			d.bringToFront(w)
			if start != "" {
				b.Navigate(start)
			}
			return
		}
	}
	if start == "" {
		start = "about:netscape"
	}
	b := NewBrowserApp(d, start)
	b.brand = netscapeBrand
	b.setupURL = start
	w := d.Open(b)
	b.win = w
}

type netscapeInstallApp struct {
	Base
	d       *Desktop
	win     *Window
	started time.Time
	done    bool
}

func (a *netscapeInstallApp) Title() string           { return "安装 Netscape Navigator 4.8" }
func (a *netscapeInstallApp) Icon() ui.IconKind       { return ui.IcoNetscape }
func (a *netscapeInstallApp) DefaultSize() (int, int) { return 440, 190 }

func (a *netscapeInstallApp) progress(now time.Time) int {
	p := int(now.Sub(a.started) / (2400 * time.Millisecond) * 100)
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	return p
}

func (a *netscapeInstallApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	p := a.progress(time.Now())
	c.TitleBar(ui.Rect{r.X, r.Y, r.W, 42}, 0x00004D, 0x00008B)
	ui.DrawIcon32(c, ui.IcoNetscape, r.X+8, r.Y+6, false)
	f.TextBold(c, r.X+50, r.Y+8, "正在安装 Netscape Navigator 4.8", ui.ColWhite)
	f.Text(c, r.X+50, r.Y+26, "目标文件夹：/usr/share/netscape48", 0xC8DCFF)

	phase := "正在复制文件…"
	switch {
	case p >= 100:
		phase = "安装完成。"
	case p >= 78:
		phase = "正在注册组件与书签…"
	case p >= 52:
		phase = "正在安装电子邮件与新闻组…"
	case p >= 26:
		phase = "正在解压 Navigator 可执行文件…"
	}
	f.Text(c, r.X+18, r.Y+58, phase, ui.ColText)
	bar := ui.Rect{r.X + 18, r.Y + 82, r.W - 36, 20}
	c.FillRect(bar, 0xFFFFFF)
	c.Sunken(bar)
	inner := ui.Rect{bar.X + 2, bar.Y + 2, (bar.W - 4) * p / 100, bar.H - 4}
	if inner.W > 0 {
		c.FillRect(inner, ui.ColNavy)
	}
	f.TextRight(c, bar.Right()-4, bar.Y, fmt.Sprintf("%d%%", p), ui.ColText)
	f.Text(c, r.X+18, r.Y+112, "这是离线模拟安装，不会下载任何内容。", ui.ColTextDis)

	if p >= 100 {
		btn := ui.Rect{r.Right() - 18 - 82, r.Bottom() - 36, 82, 26}
		ui.Button(c, btn, "完成", false, true)
	} else {
		btn := ui.Rect{r.Right() - 18 - 82, r.Bottom() - 36, 82, 26}
		ui.Button(c, btn, "取消", false, true)
	}
}

func (a *netscapeInstallApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	btn := ui.Rect{client.Right() - 18 - 82, client.Bottom() - 36, 82, 26}
	if !btn.Contains(ev.X, ev.Y) {
		return
	}
	if a.done {
		d.CloseWindow(w)
		return
	}
	d.CloseWindow(w)
	a.d.cancelNetscapeInstall()
}

func (a *netscapeInstallApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" || ev.Name == "Return" {
		if a.done {
			d.CloseWindow(w)
		} else if ev.Name == "Esc" {
			d.CloseWindow(w)
			a.d.cancelNetscapeInstall()
		}
		return true
	}
	return false
}

func (a *netscapeInstallApp) Animate(d *Desktop, w *Window, now time.Time) {
	if a.done {
		return
	}
	if a.progress(now) >= 100 {
		a.done = true
		if err := os.WriteFile(netscapeStatePath, []byte("version=4.8\ninstalled="+now.Format(time.RFC3339)+"\n"), 0o644); err != nil {
			d.MessageBox("安装 Netscape Navigator 4.8",
				"无法写入安装状态：\n"+err.Error(), IconError, []string{"确定"}, nil)
		}
		d.CloseWindow(w)
		netscapeOpenPage(d)
		return
	}
	d.Invalidate()
}

func (a *netscapeInstallApp) Close() {
	if !a.done && a.d != nil {
		a.d.cancelNetscapeInstall()
	}
}

func (d *Desktop) cancelNetscapeInstall() {
	for _, w := range d.windows {
		if _, ok := w.App.(*netscapeInstallApp); ok {
			d.removeWindow(w, false)
		}
	}
}

func (d *Desktop) NetscapeStatus() bool { return netscapeInstalled() }

func netscapeOpenPage(d *Desktop) {
	d.openNetscapeBrowser("about:netscape")
}

const netscapePageHTML = `<!doctype html>
<html><head><title>Netscape Navigator 4.8</title></head>
<body bgcolor="#ffffff" text="#000000">
<center>
<table width="100%" bgcolor="#00004d" cellpadding="8"><tr><td>
<font color="#ffffff" size="6"><b>Netscape</b></font>
<font color="#99ccff" size="4">Navigator 4.8</font>
</td></tr></table>
</center>
<h2>欢迎使用 Netscape Navigator 4.8</h2>
<p>您正在使用 Netscape Communications Corporation 于 1998 年发布的
经典浏览器。本机上的 Navigator 为 Win95 Linux 内置演示版，
窗口框架与渲染引擎均由 Go 实现。</p>
<h3>本版本包含</h3>
<ul>
<li>Navigator 浏览器 — 支持 HTML、表格、图片与书签</li>
<li>Messenger 邮件 — 与“收件箱”应用联动</li>
<li>Collabra 新闻组 — 离线新闻阅读</li>
<li>Netcaster 推送频道 — 实验特性</li>
</ul>
<h3>开始浏览</h3>
<p><a href="about:home">打开内置主页</a> ·
<a href="about:help">阅读浏览器帮助</a> ·
<a href="about:demo">查看 JavaScript 演示</a></p>
<hr>
<p><font size="2" color="#666666">Netscape Navigator 4.8 · Win95 Linux 26.5 内置演示 ·
本页面为离线内容，不会访问网络。</font></p>
</body></html>`
