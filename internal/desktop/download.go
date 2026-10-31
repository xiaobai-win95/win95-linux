package desktop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const dlFolder = "/root/下载"

type dlState int

const (
	dlIdle dlState = iota
	dlWorking
	dlDone
	dlFailed
	dlCanceled
)

type DownloaderApp struct {
	Base
	d      *Desktop
	win    *Window
	value  []rune
	caret  int
	press  string
	state  dlState
	status string
	err    string

	dest     string
	received int64
	total    int64
	speed    float64

	ctx    context.Context
	cancel context.CancelFunc

	fieldR, goR, cancelR, closeR ui.Rect
	barR                         ui.Rect
}

func (d *Desktop) openDownloader() {
	for _, w := range d.windows {
		if _, ok := w.App.(*DownloaderApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &DownloaderApp{d: d, status: "输入下载地址，然后点“开始下载”。"}
	a.win = d.Open(a)
}

func (a *DownloaderApp) Title() string     { return "下载器 - 图形化 Wget" }
func (a *DownloaderApp) Icon() ui.IconKind { return ui.IcoDownload }

func (a *DownloaderApp) DefaultSize() (int, int) { return 600, 310 }
func (a *DownloaderApp) MinSize() (int, int)     { return 600, 310 }
func (a *DownloaderApp) Resizable() bool         { return false }

func (a *DownloaderApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	ui.DrawIcon32(c, ui.IcoDownload, r.X+18, r.Y+16, false)
	f.TextBold(c, r.X+60, r.Y+20, "从 Internet 下载文件", ui.ColText)
	f.Text(c, r.X+60, r.Y+40, "文件将保存到 "+dlFolder, ui.ColShadow)

	f.Text(c, r.X+18, r.Y+70, "网址(U):", ui.ColText)
	a.fieldR = ui.Rect{r.X + 18, r.Y + 90, r.W - 36, 27}
	c.FillRect(a.fieldR, ui.ColWhite)
	c.Sunken(a.fieldR)
	editing := a.state != dlWorking
	col := ui.ColText
	if !editing {
		col = ui.ColShadow
	}
	f.TextClip(c, a.fieldR.X+5, a.fieldR.Y+5, a.fieldR.W-10, string(a.value), col)
	if editing {
		before := string(a.value[:a.caret])
		cx := a.fieldR.X + 5 + f.Width(before)
		if cx < a.fieldR.Right()-4 {
			c.VLine(cx, a.fieldR.Y+4, 19, ui.ColText)
		}
	}

	detail := a.status
	if a.state == dlWorking || a.state == dlDone {
		if a.total > 0 {
			pct := int(a.received * 100 / a.total)
			detail = fmt.Sprintf("%s / %s（%d%%）", sizeText(a.received), sizeText(a.total), pct)
		} else if a.received > 0 {
			detail = "已接收 " + sizeText(a.received)
		}
		if a.state == dlWorking && a.speed > 0 {
			detail += fmt.Sprintf("　·　%s/s", sizeText(int64(a.speed)))
		}
	}
	f.TextClip(c, r.X+18, r.Y+124, r.W-36, detail, ui.ColText)
	if a.err != "" {
		f.TextClip(c, r.X+18, r.Y+144, r.W-36, a.err, ui.ColRed)
	}

	a.barR = ui.Rect{r.X + 18, r.Y + 170, r.W - 36, 20}
	pct := 0
	if a.total > 0 {
		pct = int(a.received * 100 / a.total)
		if pct > 100 {
			pct = 100
		}
	} else if a.state == dlDone {
		pct = 100
	}
	ui.ProgressBar(c, a.barR, pct)
	if a.state == dlWorking && a.received > 0 {
		f.Text(c, a.barR.X+4, a.barR.Y+2, fmt.Sprintf("%d%%", pct), ui.ColWhite)
	}

	by := r.Bottom() - 42
	a.goR = ui.Rect{r.Right() - 292, by, 88, 26}
	a.cancelR = ui.Rect{r.Right() - 198, by, 88, 26}
	a.closeR = ui.Rect{r.Right() - 104, by, 88, 26}
	ui.Button(c, a.goR, "开始下载", a.press == "go", editing && a.state != dlWorking)
	ui.Button(c, a.cancelR, "取消", a.press == "cancel", a.state == dlWorking)
	ui.Button(c, a.closeR, "关闭", a.press == "close", true)
}

func (a *DownloaderApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.goR.Contains(ev.X, ev.Y):
			a.press = "go"
		case a.cancelR.Contains(ev.X, ev.Y):
			a.press = "cancel"
		case a.closeR.Contains(ev.X, ev.Y):
			a.press = "close"
		case a.fieldR.Contains(ev.X, ev.Y) && a.state != dlWorking:
			a.caret = runCaretAt(ui.DefaultFont(), a.value, a.fieldR, ev.X)
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "go":
			if a.goR.Contains(ev.X, ev.Y) {
				a.start()
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) {
				a.cancelDownload()
			}
		case "close":
			if a.closeR.Contains(ev.X, ev.Y) {
				a.d.CloseWindow(w)
			}
		}
	}
}

func (a *DownloaderApp) start() {
	if a.state == dlWorking {
		return
	}
	raw := strings.TrimSpace(string(a.value))
	if raw == "" {
		a.err = "请输入要下载的网址。"
		a.d.Invalidate()
		return
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		a.err = "网址无效。请使用 http:// 或 https:// 地址。"
		a.d.Invalidate()
		return
	}
	if err := os.MkdirAll(dlFolder, 0o755); err != nil {
		a.err = "无法创建下载文件夹：" + err.Error()
		a.d.Invalidate()
		return
	}
	name := downloadFileName(u)
	dest := uniqueDownloadPath(name)
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx, a.cancel = ctx, cancel
	a.dest = dest
	a.received, a.total, a.speed = 0, 0, 0
	a.err = ""
	a.state = dlWorking
	a.status = "正在连接 " + u.Host + " …"
	a.d.Invalidate()
	go a.worker(ctx, raw, dest)
}

func downloadFileName(u *url.URL) string {
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		base = "下载文件"
	}
	base = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, base)
	if strings.TrimSpace(base) == "" {
		base = "下载文件"
	}
	if len([]rune(base)) > 120 {
		base = string([]rune(base)[:120])
	}
	return base
}

func uniqueDownloadPath(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		candidate := filepath.Join(dlFolder, name)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		name = fmt.Sprintf("%s (%d)%s", stem, i, ext)
	}
	return filepath.Join(dlFolder, name)
}

func (a *DownloaderApp) worker(ctx context.Context, raw, dest string) {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		a.finishError("无法创建请求：" + err.Error())
		return
	}
	req.Header.Set("User-Agent", "Win95Linux/26.1 (wget-gui)")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			a.finishCanceled()
			return
		}
		a.finishError("连接失败：" + err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.finishError(fmt.Sprintf("服务器返回 %s。", resp.Status))
		return
	}
	total := resp.ContentLength
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		a.finishError("无法写入文件：" + err.Error())
		return
	}
	buf := make([]byte, 32*1024)
	var got int64
	lastPost := time.Now()
	lastBytes := int64(0)
	lastAt := time.Now()
	post := func(done bool) {
		g, t := got, total
		var speed float64
		now := time.Now()
		if elapsed := now.Sub(lastAt).Seconds(); elapsed > 0.05 {
			speed = float64(g-lastBytes) / elapsed
			lastAt, lastBytes = now, g
		}
		a.d.Post(func() {
			a.received, a.total = g, t
			if speed > 0 {
				a.speed = speed
			}
			if !done {
				a.status = "正在下载 " + filepath.Base(dest) + " …"
			}
			a.d.Invalidate()
		})
	}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				a.finishError("写入文件失败：" + werr.Error())
				return
			}
			got += int64(n)
			if time.Since(lastPost) >= 200*time.Millisecond {
				lastPost = time.Now()
				post(false)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			if ctx.Err() == context.Canceled {
				a.finishCanceled()
			} else {
				a.finishError("下载中断：" + rerr.Error())
			}
			return
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		a.finishError("保存文件失败：" + err.Error())
		return
	}
	if ctx.Err() == context.Canceled {
		os.Remove(tmp)
		a.finishCanceled()
		return
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		a.finishError("保存文件失败：" + err.Error())
		return
	}
	saved := got
	a.d.Post(func() {
		a.state = dlDone
		a.received, a.total = saved, saved
		a.speed = 0
		a.status = fmt.Sprintf("下载完成：%s（%s）", filepath.Base(dest), sizeText(saved))
		a.err = ""
		a.d.Notify("下载完成", filepath.Base(dest)+" 已保存到 "+dlFolder)
		a.d.Invalidate()
	})
}

func (a *DownloaderApp) finishError(msg string) {
	a.d.Post(func() {
		a.state = dlFailed
		a.speed = 0
		a.err = msg
		a.status = "下载失败。"
		a.d.Notify("下载失败", msg)
		a.d.Invalidate()
	})
}

func (a *DownloaderApp) finishCanceled() {
	a.d.Post(func() {
		a.state = dlCanceled
		a.speed = 0
		a.status = "下载已取消，未完成的临时文件已删除。"
		a.err = ""
		a.d.Invalidate()
	})
}

func (a *DownloaderApp) cancelDownload() {
	if a.state != dlWorking {
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.status = "正在取消…"
	a.d.Invalidate()
}

func (a *DownloaderApp) RequestClose(d *Desktop, w *Window) bool {
	if a.state != dlWorking {
		return true
	}
	d.MessageBox("下载器", "文件正在下载，关闭窗口会取消下载。\n\n仍要关闭吗？", IconQuestion,
		[]string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			a.cancelDownload()
			d.CloseWindowForced(w)
		})
	return false
}

func (a *DownloaderApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.state == dlWorking {
		if ev.Name == "Esc" {
			a.cancelDownload()
			return true
		}
		return false
	}
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Enter":
		a.start()
		return true
	case "Backspace":
		if a.caret > 0 {
			a.value = append(a.value[:a.caret-1], a.value[a.caret:]...)
			a.caret--
			a.err = ""
		}
		return true
	case "Delete":
		if a.caret < len(a.value) {
			a.value = append(a.value[:a.caret], a.value[a.caret+1:]...)
			a.err = ""
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
		}
		return true
	case "Right":
		if a.caret < len(a.value) {
			a.caret++
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(a.value)
		return true
	case "F5":
		a.start()
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.value = append(a.value, 0)
		copy(a.value[a.caret+1:], a.value[a.caret:])
		a.value[a.caret] = ev.Rune
		a.caret++
		a.err = ""
		return true
	}
	return false
}

func (a *DownloaderApp) Close() {
	if a.cancel != nil {
		a.cancel()
	}
}
