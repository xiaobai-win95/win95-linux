package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/skip2/go-qrcode"

	"win95linux/internal/lanshare"
	"win95linux/internal/ui"
)

func (d *Desktop) ensureShare() (*lanshare.Server, error) {
	if d.share == nil {
		d.share = lanshare.New(dlFolder)
	}
	if d.share.Running() {
		return d.share, nil
	}
	if err := d.share.Start(); err != nil {
		return d.share, err
	}
	return d.share, nil
}

func (d *Desktop) lanTrayRect() ui.Rect {
	tray := d.sysTrayRect()
	return ui.Rect{tray.X - 30, tray.Y, 26, tray.H}
}

func (d *Desktop) drawLanTray(c *ui.Canvas) {
	r := d.lanTrayRect()
	active := d.share != nil && d.share.Running()
	if active {
		c.FillRect(r, ui.ColFace)
	} else {
		c.FillRect(r, ui.ColFace)
	}
	c.Sunken(r)
	ui.DrawIcon16(c, ui.IcoShare, r.X+5, r.Y+4)
	if active {
		c.Set(r.X+20, r.Y+3, 0x008000)
	}
}

func (d *Desktop) openLANShare() {
	srv, err := d.ensureShare()
	if err != nil {
		d.MessageBox("局域网快传", "服务器启动失败：\n\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	for _, w := range d.windows {
		if a, ok := w.App.(*ShareApp); ok && a.mode == 0 {
			a.srv = srv
			a.link = srv.URL()
			d.bringToFront(w)
			d.Invalidate()
			return
		}
	}
	a := &ShareApp{
		d:       d,
		srv:     srv,
		mode:    0,
		link:    srv.URL(),
		heading: "局域网快传",
		note:    "手机和这台电脑连同一个 Wi-Fi，扫码即可上传或下载文件。",
	}
	a.win = d.Open(a)
}

func (d *Desktop) shareFile(path string) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		d.MessageBox("发送到手机", "请选择一个普通文件。", IconInfo, []string{"确定"}, nil)
		return
	}
	srv, err := d.ensureShare()
	if err != nil {
		d.MessageBox("局域网快传", "服务器启动失败：\n\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	link, err := srv.RegisterFile(path)
	if err != nil {
		d.MessageBox("发送到手机", "无法生成下载链接：\n\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a := &ShareApp{
		d:       d,
		srv:     srv,
		mode:    1,
		link:    link,
		heading: "发送到手机",
		note:    filepath.Base(path) + "  ·  " + sizeText(st.Size()),
	}
	a.win = d.Open(a)
}

func (d *Desktop) shareTrayMenu() []ctxItem {
	running := d.share != nil && d.share.Running()
	items := []ctxItem{
		{label: "打开局域网快传", icon: ui.IcoShare, enabled: true, action: func() { d.openLANShare() }},
	}
	if running {
		url := d.share.URL()
		items = append(items,
			ctxItem{label: "复制共享地址", icon: ui.IcoNetwork, enabled: true, action: func() {
				clipboardWrite(url)
				d.Notify("局域网快传", "地址已复制："+url)
			}},
			ctxItem{label: "打开共享文件夹", icon: ui.IcoFolderOpen, enabled: true, action: func() { d.openFiles(dlFolder) }},
			ctxItem{sep: true},
			ctxItem{label: "停止共享服务", icon: ui.IcoNetwork, enabled: true, action: func() {
				d.share.Stop()
				d.Notify("局域网快传", "共享服务已停止")
				d.Invalidate()
			}},
		)
	} else {
		items = append(items, ctxItem{label: "启动共享服务", icon: ui.IcoNetwork, enabled: true, action: func() {
			if _, err := d.ensureShare(); err != nil {
				d.MessageBox("局域网快传", "服务器启动失败：\n\n"+err.Error(), IconError, []string{"确定"}, nil)
			} else {
				d.Notify("局域网快传", "共享地址："+d.share.URL())
			}
			d.Invalidate()
		}})
	}
	return items
}

type ShareApp struct {
	Base
	d   *Desktop
	win *Window

	srv     *lanshare.Server
	mode    int
	link    string
	heading string
	note    string

	press    string
	lastUp   int64
	qrR      ui.Rect
	btnCopy  ui.Rect
	btnOpen  ui.Rect
	btnClose ui.Rect
}

func (a *ShareApp) Title() string {
	if a.mode == 1 {
		return "发送到手机 - " + a.heading
	}
	return "局域网快传"
}

func (a *ShareApp) Icon() ui.IconKind {
	if a.mode == 1 {
		return ui.IcoShare
	}
	return ui.IcoNetwork
}

func (a *ShareApp) DefaultSize() (int, int) {
	if a.mode == 1 {
		return 460, 560
	}
	return 520, 610
}

func (a *ShareApp) MinSize() (int, int) { w, h := a.DefaultSize(); return w, h }
func (a *ShareApp) Resizable() bool     { return false }
func (a *ShareApp) Close()              {}

func (a *ShareApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	ui.DrawIcon32(c, ui.IcoShare, r.X+18, r.Y+14, false)
	f.TextBold(c, r.X+62, r.Y+16, a.heading, ui.ColText)
	if a.mode == 1 {
		f.Text(c, r.X+62, r.Y+36, a.note, ui.ColShadow)
	} else {
		f.Text(c, r.X+62, r.Y+36, "手机扫码，与这台电脑互传文件", ui.ColShadow)
	}

	urlBox := ui.Rect{r.X + 18, r.Y + 62, r.W - 36, 24}
	c.FillRect(urlBox, ui.ColField)
	c.Sunken(urlBox)
	f.TextClip(c, urlBox.X+6, urlBox.Y+4, urlBox.W-12, a.link, ui.ColNavy)

	qrSize := r.W - 92
	if qrSize > 296 {
		qrSize = 296
	}
	if a.mode == 0 && r.H < 610 {
		qrSize = minInt(qrSize, r.H-260)
	}
	if qrSize < 150 {
		qrSize = 150
	}
	a.qrR = ui.Rect{r.X + (r.W-qrSize)/2, urlBox.Bottom() + 14, qrSize, qrSize}
	c.FillRect(a.qrR, ui.ColWhite)
	c.Sunken(a.qrR)
	if err := drawQR(c, a.link, ui.Rect{a.qrR.X + 8, a.qrR.Y + 8, a.qrR.W - 16, a.qrR.H - 16}); err != nil {
		f.TextClip(c, a.qrR.X+10, a.qrR.Y+10, a.qrR.W-20, "二维码生成失败："+err.Error(), ui.ColRed)
	}

	infoY := a.qrR.Bottom() + 10
	if a.mode == 0 {
		uploads, last, _, upErr := a.srv.Stats()
		info := fmt.Sprintf("共享文件夹：%s", dlFolder)
		f.TextClip(c, r.X+18, infoY, r.W-36, info, ui.ColText)
		infoY += 18
		if upErr != "" {
			f.TextClip(c, r.X+18, infoY, r.W-36, "最近错误："+upErr, ui.ColRed)
		} else if uploads > 0 {
			f.TextClip(c, r.X+18, infoY, r.W-36, fmt.Sprintf("已接收 %d 个文件，最近：%s", uploads, last), ui.ColGreen)
		} else {
			f.Text(c, r.X+18, infoY, "等待手机上传…", ui.ColShadow)
		}
	} else {
		f.TextClip(c, r.X+18, infoY, r.W-36, "用手机相机扫码，即可下载这个文件。", ui.ColText)
		infoY += 18
		f.TextClip(c, r.X+18, infoY, r.W-36, "链接只在本次运行期间有效。", ui.ColShadow)
	}

	by := r.Bottom() - 36
	a.btnCopy = ui.Rect{r.Right() - 314, by, 92, 26}
	a.btnOpen = ui.Rect{r.Right() - 216, by, 92, 26}
	a.btnClose = ui.Rect{r.Right() - 118, by, 92, 26}
	if a.mode == 1 {
		a.btnOpen = ui.Rect{0, 0, 0, 0}
		a.btnCopy = ui.Rect{r.Right() - 216, by, 104, 26}
	}
	ui.Button(c, a.btnCopy, "复制链接", a.press == "copy", true)
	if a.mode == 0 {
		ui.Button(c, a.btnOpen, "打开页面", a.press == "open", a.link != "")
	}
	ui.Button(c, a.btnClose, "关闭", a.press == "close", true)
}

func (a *ShareApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnCopy.Contains(ev.X, ev.Y):
			a.press = "copy"
		case a.mode == 0 && a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnClose.Contains(ev.X, ev.Y):
			a.press = "close"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "copy":
			if a.btnCopy.Contains(ev.X, ev.Y) {
				clipboardWrite(a.link)
				d.Notify("局域网快传", "链接已复制："+a.link)
			}
		case "open":
			if a.btnOpen.Contains(ev.X, ev.Y) {
				d.openBrowser(a.link)
			}
		case "close":
			if a.btnClose.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
	}
}

func (a *ShareApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Enter":
		clipboardWrite(a.link)
		d.Notify("局域网快传", "链接已复制："+a.link)
		return true
	}
	return false
}

func (a *ShareApp) Tick(d *Desktop, w *Window, now time.Time) {
	if a.mode != 0 {
		return
	}
	uploads, _, _, _ := a.srv.Stats()
	if uploads != a.lastUp {
		a.lastUp = uploads
		d.Invalidate()
	}
}

func drawQR(c *ui.Canvas, link string, r ui.Rect) error {
	q, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return err
	}
	q.DisableBorder = true
	bmp := q.Bitmap()
	n := len(bmp)
	if n == 0 {
		return fmt.Errorf("二维码为空")
	}
	const quiet = 2
	total := n + quiet*2
	scale := r.W / total
	if s := r.H / total; s < scale {
		scale = s
	}
	if scale < 1 {
		scale = 1
	}
	size := total * scale
	ox := r.X + (r.W-size)/2
	oy := r.Y + (r.H-size)/2
	c.FillRect(ui.Rect{ox, oy, size, size}, ui.ColWhite)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if bmp[y][x] {
				c.FillRect(ui.Rect{ox + (x+quiet)*scale, oy + (y+quiet)*scale, scale, scale}, 0x000000)
			}
		}
	}
	return nil
}
