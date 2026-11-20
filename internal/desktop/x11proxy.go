package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"win95linux/internal/ui"
)

const (
	x11StateStarting = iota
	x11StateInstalling
	x11StateReady
	x11StateFailed
)

// X11ProxyApp shows the Xvfb :1 screen inside a Win95 window and forwards
// mouse and keyboard input back through XTest.
type X11ProxyApp struct {
	Base
	d   *Desktop
	win *Window

	sess    *x11Session
	state   int
	errMsg  string
	pending []string

	starting     bool
	installAsk   bool
	installMsg   string
	installReady bool

	frame     []uint32
	frameW    int
	frameH    int
	frameHash uint64
	frameMu   sync.Mutex
	stopCh    chan struct{}
	stopOnce  sync.Once
	captureOn bool

	screenR                           ui.Rect
	btnTerm, btnFirefox, btnReconnect ui.Rect
	press                             string
	lastX, lastY                      int
	havePointer, down                 bool
	lastMove                          time.Time
}

func NewX11ProxyApp(d *Desktop, pending []string) *X11ProxyApp {
	a := &X11ProxyApp{
		d:       d,
		pending: pending,
		state:   x11StateStarting,
		lastX:   -1,
		lastY:   -1,
	}
	a.begin()
	return a
}

func (a *X11ProxyApp) Title() string           { return "X11 代理 - :1" }
func (a *X11ProxyApp) Icon() ui.IconKind       { return ui.IcoTerminal }
func (a *X11ProxyApp) DefaultSize() (int, int) { return 860, 720 }
func (a *X11ProxyApp) MinSize() (int, int)     { return 640, 520 }
func (a *X11ProxyApp) Resizable() bool         { return true }

func (a *X11ProxyApp) begin() {
	if a.starting {
		return
	}
	a.starting = true
	go func() {
		sess, err := x11EnsureSession()
		a.d.Post(func() {
			a.starting = false
			if err != nil {
				if errors.Is(err, errX11Missing) {
					a.installReady = false
					a.askInstall()
				} else {
					a.state = x11StateFailed
					a.errMsg = err.Error()
				}
			} else {
				a.attach(sess)
			}
			a.d.Invalidate()
		})
	}()
}

func (a *X11ProxyApp) attach(sess *x11Session) {
	a.sess = sess
	a.state = x11StateReady
	a.errMsg = ""
	a.installMsg = ""
	a.startCapture()
	if len(a.pending) > 0 {
		argv := a.pending
		a.pending = nil
		a.launchArgv(argv)
	}
}

func (a *X11ProxyApp) askInstall() {
	a.state = x11StateInstalling
	a.errMsg = "需要安装 Xvfb 与图形应用组件"
	if a.installAsk {
		return
	}
	a.installAsk = true
	a.d.MessageBox("X11 嵌套代理",
		"内嵌 X11 需要以下组件：\n\n"+
			"· Xvfb 虚拟显示服务器\n"+
			"· xfce4-terminal 终端\n"+
			"· Firefox 浏览器\n"+
			"· 中文字体与运行库\n\n"+
			"首次安装需要联网，并从 Alpine 镜像下载约 300–500MB。\n"+
			"安装在后台进行，完成后会自动连接 :1 并启动应用，\n"+
			"不会切换到 tty2，也不会启动完整的 Xfce 桌面。",
		IconQuestion, []string{"安装", "取消"}, func(i int) {
			if i != 0 {
				a.installAsk = false
				a.pending = nil
				if a.sess != nil {
					a.state = x11StateReady
					a.errMsg = ""
				} else {
					a.state = x11StateFailed
					a.errMsg = "已取消安装 X11 组件"
				}
				a.d.Invalidate()
				return
			}
			a.startInstaller()
		})
}

func (a *X11ProxyApp) startInstaller() {
	if _, err := os.Stat("/usr/bin/win95-x11-setup"); err != nil {
		a.state = x11StateFailed
		a.errMsg = "镜像缺少 win95-x11-setup 安装脚本"
		a.d.Invalidate()
		return
	}
	a.d.openInstallStatus("X11 嵌套代理组件")
	_ = os.Remove(installStatusPath)
	cmd := exec.Command("/usr/bin/win95-x11-setup")
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
	}
	if err := cmd.Start(); err != nil {
		a.state = x11StateFailed
		a.errMsg = "无法启动安装任务：" + err.Error()
		a.d.Invalidate()
		return
	}
	_ = cmd.Process.Release()
	a.installMsg = "正在更新软件包索引…"
	a.d.Invalidate()
}

func (a *X11ProxyApp) installDone() bool {
	data, err := os.ReadFile(installStatusPath)
	if err != nil {
		return false
	}
	_, _, msg, state := parseInstallStatus(string(data))
	if msg != "" {
		a.installMsg = msg
	}
	switch state {
	case "running":
		return false
	case "failed":
		a.state = x11StateFailed
		a.errMsg = "X11 组件安装失败：" + msg + "\n详情见安装进度窗口。"
		a.d.Invalidate()
		return false
	case "done":
		if !x11XvfbPresent() {
			a.state = x11StateFailed
			a.errMsg = "安装已结束，但没有找到 /usr/bin/Xvfb。"
			a.d.Invalidate()
			return false
		}
		return true
	}
	return false
}

func x11XvfbPresent() bool {
	if st, err := os.Stat("/usr/bin/Xvfb"); err == nil && !st.IsDir() {
		return true
	}
	return false
}

func (a *X11ProxyApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if a.state == x11StateInstalling && !a.starting {
		if a.installDone() {
			a.state = x11StateStarting
			a.installMsg = ""
			a.installAsk = false
			a.begin()
			d.Invalidate()
		} else if a.installMsg != "" {
			d.Invalidate()
		}
	}
}

func (a *X11ProxyApp) startCapture() {
	if a.captureOn {
		return
	}
	stop := make(chan struct{})
	a.stopCh = stop
	a.captureOn = true
	go func(sess *x11Session) {
		ticker := time.NewTicker(110 * time.Millisecond)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			ticks++
			if ticks%10 == 1 {
				sess.fitWindows()
			}
			pix, hash, err := sess.Capture()
			if err != nil {
				a.d.Post(func() {
					if a.state == x11StateReady {
						a.state = x11StateFailed
						a.errMsg = "画面捕获失败：" + err.Error()
						a.captureOn = false
					}
					a.d.Invalidate()
				})
				return
			}
			a.frameMu.Lock()
			changed := hash != a.frameHash || len(a.frame) == 0
			if changed {
				a.frame = pix
				a.frameW = sess.width
				a.frameH = sess.height
				a.frameHash = hash
			}
			a.frameMu.Unlock()
			if changed {
				a.d.Post(func() { a.d.Invalidate() })
			}
		}
	}(a.sess)
}

func (a *X11ProxyApp) stopCapture() {
	if !a.captureOn {
		return
	}
	a.captureOn = false
	a.stopOnce.Do(func() {
		if a.stopCh != nil {
			close(a.stopCh)
		}
	})
}

func (a *X11ProxyApp) Close() {
	a.stopCapture()
}

func (a *X11ProxyApp) Resized(d *Desktop, w *Window, client ui.Rect) {}

func (a *X11ProxyApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	ty := r.Y + 4
	a.btnTerm = ui.Rect{r.X + 4, ty, 88, 24}
	a.btnFirefox = ui.Rect{a.btnTerm.Right() + 4, ty, 88, 24}
	a.btnReconnect = ui.Rect{a.btnFirefox.Right() + 10, ty, 82, 24}
	ui.Button(c, a.btnTerm, "新建终端", a.press == "term", a.sess != nil)
	ui.Button(c, a.btnFirefox, "Firefox", a.press == "firefox", a.sess != nil)
	ui.Button(c, a.btnReconnect, "重新连接", a.press == "retry", a.state != x11StateStarting)
	switch a.state {
	case x11StateReady:
		f.TextRight(c, r.Right()-8, ty+4, "Xvfb :1 · 1024×768 · 点击画面即可操作", ui.ColShadow)
	case x11StateInstalling:
		f.TextRight(c, r.Right()-8, ty+4, "正在安装组件…", ui.ColText)
	default:
		f.TextRight(c, r.Right()-8, ty+4, "未连接", ui.ColShadow)
	}
	c.HLine(r.X+4, r.Y+31, r.W-8, ui.ColShadow)
	c.HLine(r.X+4, r.Y+32, r.W-8, ui.ColWhite)

	statusH := 22
	a.screenR = ui.Rect{}
	area := ui.Rect{r.X + 4, r.Y + 36, r.W - 8, r.H - 36 - statusH}
	if area.H < 20 {
		area.H = 20
	}
	c.FillRect(area, 0)
	c.Sunken(area)

	ready := a.state == x11StateReady && a.sess != nil
	if ready {
		a.frameMu.Lock()
		frame := a.frame
		fw, fh := a.frameW, a.frameH
		a.frameMu.Unlock()
		if len(frame) >= fw*fh && fw > 0 && fh > 0 {
			tw, th := fw, fh
			if area.W < fw || area.H < fh {
				scaleW := area.W * 1000 / fw
				scaleH := area.H * 1000 / fh
				scale := scaleW
				if scaleH < scale {
					scale = scaleH
				}
				if scale < 1 {
					scale = 1
				}
				tw = fw * scale / 1000
				th = fh * scale / 1000
			}
			if tw < 1 {
				tw = 1
			}
			if th < 1 {
				th = 1
			}
			x0 := area.X + (area.W-tw)/2
			y0 := area.Y + (area.H-th)/2
			target := ui.Rect{x0, y0, tw, th}
			a.blit(c, target, frame, fw, fh)
			c.Frame(target, ui.ColShadow)
			a.screenR = target
		}
	}

	if a.screenR.W == 0 {
		msg := "正在启动 Xvfb :1…"
		switch a.state {
		case x11StateInstalling:
			msg = "正在安装 X11 组件…\n" + a.installMsg + "\n\n进度见“安装进度”窗口，完成后自动连接。"
		case x11StateFailed:
			msg = "无法显示 X11 桌面：\n" + a.errMsg
		}
		lines := splitStatusLines(msg)
		startY := area.Y + area.H/2 - len(lines)*10
		for i, line := range lines {
			col := ui.ColWhite
			if a.state == x11StateFailed && i == 0 {
				col = 0xFF8080
			}
			lw := f.Width(line)
			f.Text(c, area.X+(area.W-lw)/2, startY+i*20, line, col)
		}
	}

	a.statusBar(c, f, r, statusH)
}

func splitStatusLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func (a *X11ProxyApp) blit(c *ui.Canvas, target ui.Rect, frame []uint32, fw, fh int) {
	tw, th := target.W, target.H
	if tw == fw && th == fh {
		for y := 0; y < th; y++ {
			dst := (target.Y+y)*c.Stride + target.X
			copy(c.Pix[dst:dst+tw], frame[y*fw:(y+1)*fw])
		}
		return
	}
	xmap := make([]int, tw)
	for x := 0; x < tw; x++ {
		xmap[x] = x * fw / tw
	}
	for y := 0; y < th; y++ {
		src := frame[(y*fh/th)*fw:]
		dst := (target.Y+y)*c.Stride + target.X
		for x := 0; x < tw; x++ {
			c.Pix[dst+x] = src[xmap[x]]
		}
	}
}

func (a *X11ProxyApp) statusBar(c *ui.Canvas, f *ui.Font, r ui.Rect, statusH int) {
	sr := ui.Rect{r.X, r.Bottom() - statusH, r.W, statusH}
	c.FillRect(sr, ui.ColFace)
	c.HLine(sr.X, sr.Y, sr.W, ui.ColShadow)
	c.HLine(sr.X, sr.Y+1, sr.W, ui.ColWhite)
	text := "就绪"
	switch a.state {
	case x11StateStarting:
		text = "正在启动 Xvfb :1…"
	case x11StateInstalling:
		text = "正在安装 X11 组件…"
	case x11StateFailed:
		text = "错误：" + oneLine(a.errMsg)
	case x11StateReady:
		if a.pending == nil {
			text = "Xvfb :1 · 鼠标和键盘已转发到虚拟桌面"
		}
	}
	f.TextClip(c, sr.X+6, sr.Y+4, sr.W-180, text, ui.ColText)
	if a.havePointer && a.state == x11StateReady {
		f.TextRight(c, sr.Right()-6, sr.Y+4, fmt.Sprintf("X:%d  Y:%d", a.lastX, a.lastY), ui.ColShadow)
	}
}

func (a *X11ProxyApp) toX11(x, y int) (int, int, bool) {
	if a.sess == nil || a.screenR.W <= 0 || a.screenR.H <= 0 {
		return 0, 0, false
	}
	r := a.screenR
	if !r.Contains(x, y) {
		return 0, 0, false
	}
	sx := (x - r.X) * a.sess.width / r.W
	sy := (y - r.Y) * a.sess.height / r.H
	if sx < 0 {
		sx = 0
	}
	if sy < 0 {
		sy = 0
	}
	if sx >= a.sess.width {
		sx = a.sess.width - 1
	}
	if sy >= a.sess.height {
		sy = a.sess.height - 1
	}
	return sx, sy, true
}

func (a *X11ProxyApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		if a.state != x11StateReady || a.sess == nil {
			return
		}
		if sx, sy, ok := a.toX11(ev.X, ev.Y); ok {
			a.sess.Wheel(sx, sy, ev.Wheel)
			a.lastX, a.lastY = sx, sy
			a.havePointer = true
			d.Invalidate()
		}
		return
	}
	switch ev.Kind {
	case MouseMove:
		if a.state != x11StateReady || a.sess == nil {
			return
		}
		if sx, sy, ok := a.toX11(ev.X, ev.Y); ok {
			if time.Since(a.lastMove) >= 15*time.Millisecond || !a.havePointer {
				a.sess.Move(sx, sy)
				a.lastMove = time.Now()
			}
			a.lastX, a.lastY = sx, sy
			a.havePointer = true
			d.Invalidate()
		}
	case MouseDown:
		a.press = ""
		switch {
		case a.btnTerm.Contains(ev.X, ev.Y):
			a.press = "term"
		case a.btnFirefox.Contains(ev.X, ev.Y):
			a.press = "firefox"
		case a.btnReconnect.Contains(ev.X, ev.Y):
			a.press = "retry"
		default:
			if a.state == x11StateReady && a.sess != nil {
				if sx, sy, ok := a.toX11(ev.X, ev.Y); ok {
					a.sess.focus()
					a.sess.Button(true, 1, sx, sy)
					a.lastX, a.lastY = sx, sy
					a.havePointer = true
					a.down = true
					d.Invalidate()
				}
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		if a.down && a.sess != nil {
			sx, sy := a.lastX, a.lastY
			if px, py, ok := a.toX11(ev.X, ev.Y); ok {
				sx, sy = px, py
			}
			a.sess.Button(false, 1, sx, sy)
			a.down = false
			a.lastX, a.lastY = sx, sy
			a.d.Invalidate()
			return
		}
		switch p {
		case "term":
			if a.btnTerm.Contains(ev.X, ev.Y) {
				a.launch("xfce4-terminal")
			}
		case "firefox":
			if a.btnFirefox.Contains(ev.X, ev.Y) {
				a.launch("firefox")
			}
		case "retry":
			if a.btnReconnect.Contains(ev.X, ev.Y) {
				a.reconnect()
			}
		}
	}
}

func (a *X11ProxyApp) RightClick(d *Desktop, w *Window, x, y int) bool {
	if a.state != x11StateReady || a.sess == nil {
		return false
	}
	sx, sy, ok := a.toX11(x, y)
	if !ok {
		return false
	}
	a.sess.focus()
	a.sess.Button(true, 3, sx, sy)
	a.sess.Button(false, 3, sx, sy)
	a.lastX, a.lastY = sx, sy
	a.havePointer = true
	d.Invalidate()
	return true
}

func (a *X11ProxyApp) reconnect() {
	if a.starting {
		return
	}
	a.state = x11StateStarting
	a.errMsg = ""
	a.installAsk = false
	if a.sess != nil {
		a.stopCapture()
		a.sess = nil
	}
	a.begin()
}

func (a *X11ProxyApp) launch(app string) {
	a.launchArgv([]string{app})
}

func (a *X11ProxyApp) launchArgv(argv []string) {
	if len(argv) == 0 {
		return
	}
	if a.state != x11StateReady || a.sess == nil {
		a.pending = append([]string{}, argv...)
		return
	}
	if err := a.sess.Launch(argv[0], argv[1:]...); err != nil {
		var missing *x11AppMissingError
		if errors.As(err, &missing) {
			a.pending = append([]string{}, argv...)
			a.installAsk = false
			a.askInstall()
			return
		}
		a.d.MessageBox("X11 代理", "无法启动 "+argv[0]+"：\n"+err.Error(), IconError, []string{"确定"}, nil)
	}
}

// launchShell runs a shortcut command inside the virtual display. The script
// inherits DISPLAY=:1 and lives in /tmp, so the desktop does not have to parse
// Exec= quoting rules beyond what the shell already understands.
func (a *X11ProxyApp) launchShell(execLine string, terminal bool) {
	execLine = strings.TrimSpace(execLine)
	if execLine == "" {
		return
	}
	script := filepath.Join("/tmp", fmt.Sprintf("win95-x11-shortcut-%d.sh", time.Now().UnixNano()))
	body := "#!/bin/sh\ncd /root\nexec " + execLine + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		a.d.MessageBox("X11 代理", "无法创建启动脚本：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	if terminal {
		a.launchArgv([]string{"xfce4-terminal", "--command=" + script})
		return
	}
	a.launchArgv([]string{"/bin/sh", script})
}

func (a *X11ProxyApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.state != x11StateReady || a.sess == nil {
		return false
	}
	a.sess.Key(ev)
	d.Invalidate()
	return true
}

func (a *X11ProxyApp) launchChecked(app string, extra ...string) {
	if x11AppPresent(app) {
		a.launchArgv(append([]string{app}, extra...))
		return
	}
	a.pending = append([]string{app}, extra...)
	a.installAsk = false
	a.askInstall()
}

func x11AppPresent(app string) bool {
	if p, err := exec.LookPath(app); err == nil && p != "" {
		return true
	}
	st, err := os.Stat(filepath.Join("/usr/bin", app))
	return err == nil && !st.IsDir()
}

// openX11Proxy opens (or reuses) the proxy window and optionally starts an
// application inside it.
func (d *Desktop) openX11Proxy(app string) {
	d.openX11ProxyArgs(app, nil)
}

func (d *Desktop) openX11ProxyArgs(app string, args []string) {
	d.x11ProxyWindow(app, args)
}

func (d *Desktop) x11ProxyWindow(app string, args []string) *X11ProxyApp {
	for _, w := range d.windows {
		if proxy, ok := w.App.(*X11ProxyApp); ok {
			d.bringToFront(w)
			if app != "" {
				proxy.launchChecked(app, args...)
			}
			return proxy
		}
	}
	pending := []string(nil)
	if app != "" {
		pending = append([]string{app}, args...)
	}
	px := NewX11ProxyApp(d, pending)
	w := d.Open(px)
	px.win = w
	return px
}

var _ ClientContextMenu = (*X11ProxyApp)(nil)

func (a *X11ProxyApp) ContextMenu(d *Desktop, w *Window, x, y int) []ctxItem {
	if a.screenR.W > 0 && a.screenR.Contains(x, y) {
		return nil
	}
	return []ctxItem{
		{label: "新建终端(T)", icon: ui.IcoTerminal, enabled: a.state == x11StateReady, action: func() { a.launch("xfce4-terminal") }},
		{label: "Firefox(B)", icon: ui.IcoBrowser, enabled: a.state == x11StateReady, action: func() { a.launch("firefox") }},
		{sep: true},
		{label: "重新连接(R)", enabled: a.state != x11StateStarting, action: func() { a.reconnect() }},
	}
}
