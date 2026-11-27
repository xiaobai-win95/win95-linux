package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
	"win95linux/internal/vnc"
)

func (d *Desktop) vncServer() *vnc.Server {
	if d.vnc == nil {
		d.vnc = vnc.New(vnc.Config{
			Width:  d.W,
			Height: d.H,
			Name:   "Windows 95 Linux 26.5",
			OnPointer: func(x, y int, mask, prev uint8) {
				events := vncPointerEvents(d.W, d.H, x, y, mask, prev)
				if len(events) == 0 {
					return
				}
				d.Post(func() {
					for _, ev := range events {
						d.Mouse(ev)
					}
				})
			},
			OnKey: func(down bool, keysym uint32, shift, ctrl, alt bool) {
				if !down {
					return
				}
				ev, ok := keysymEvent(keysym, shift, ctrl, alt)
				if !ok {
					return
				}
				d.Post(func() { d.Key(ev) })
			},
			OnClipboard: func(text string) {
				d.Post(func() { clipboardWrite(text) })
			},
			OnClients: func(n int) {
				d.Post(func() { d.Invalidate() })
			},
		})
	}
	return d.vnc
}

// PushFrame is called by the GUI after each canvas repaint; it is a no-op
// unless a VNC client is connected.
func (d *Desktop) PushFrame(pix []uint32, stride, w, h int) {
	if d.vnc != nil && d.vnc.Running() {
		d.vnc.PushFrame(pix, stride, w, h)
	}
}

func vncPointerEvents(w, h, x, y int, mask, prev uint8) []MouseEvent {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= w {
		x = w - 1
	}
	if y >= h {
		y = h - 1
	}
	move := MouseEvent{Kind: MouseMove, X: x, Y: y}
	var out []MouseEvent
	out = append(out, move)
	button := func(bit uint8) int {
		switch bit {
		case 1:
			return 1
		case 2:
			return 2
		case 4:
			return 3
		}
		return 0
	}
	pressed := mask &^ prev
	released := prev &^ mask
	for _, b := range []uint8{1, 2, 4} {
		if pressed&b != 0 {
			out = append(out, MouseEvent{Kind: MouseDown, X: x, Y: y, Button: button(b)})
		}
		if released&b != 0 {
			out = append(out, MouseEvent{Kind: MouseUp, X: x, Y: y, Button: button(b)})
		}
	}
	if pressed&8 != 0 {
		out = append(out, MouseEvent{Kind: MouseWheel, X: x, Y: y, Wheel: 1})
	}
	if pressed&16 != 0 {
		out = append(out, MouseEvent{Kind: MouseWheel, X: x, Y: y, Wheel: -1})
	}
	return out
}

func keysymEvent(ks uint32, shift, ctrl, alt bool) (KeyEvent, bool) {
	ev := KeyEvent{Shift: shift, Ctrl: ctrl, Alt: alt}
	switch ks {
	case 0xff0d, 0xff8d:
		ev.Name = "Enter"
	case 0xff08:
		ev.Name = "Backspace"
	case 0xff09, 0xfe20:
		ev.Name = "Tab"
	case 0xff1b:
		ev.Name = "Esc"
	case 0xffff:
		ev.Name = "Delete"
	case 0xff63:
		ev.Name = "Insert"
	case 0xff51:
		ev.Name = "Left"
	case 0xff52:
		ev.Name = "Up"
	case 0xff53:
		ev.Name = "Right"
	case 0xff54:
		ev.Name = "Down"
	case 0xff50:
		ev.Name = "Home"
	case 0xff57:
		ev.Name = "End"
	case 0xff55:
		ev.Name = "PageUp"
	case 0xff56:
		ev.Name = "PageDown"
	case 0xff67:
		ev.Name = "Menu"
	case 0xff61:
		ev.Name = "Print"
	case 0xffe5:
		ev.Name = "CapsLock"
	case 0xff7f:
		ev.Name = "NumLock"
	case 0xff14:
		ev.Name = "ScrollLock"
	case 0xff13:
		ev.Name = "Pause"
	case 0xff6b:
		ev.Name = "Cancel"
	default:
		if ks >= 0xffbe && ks <= 0xffc9 {
			ev.Name = fmt.Sprintf("F%d", int(ks-0xffbe)+1)
		} else if ks >= 0xff80 && ks <= 0xffb9 {
			if name, r, ok := keypadKey(ks); ok {
				ev.Name = name
				ev.Rune = r
			} else {
				return KeyEvent{}, false
			}
		} else if ks >= 0x20 && ks <= 0x7e {
			r := rune(ks)
			if shift && r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			ev.Rune = r
			if r == ' ' {
				ev.Name = "Space"
			}
		} else if ks&0xff000000 == 0x01000000 {
			r := rune(ks & 0x00ffffff)
			if !validRune(r) {
				return KeyEvent{}, false
			}
			ev.Rune = r
		} else {
			return KeyEvent{}, false
		}
	}
	return ev, true
}

func keypadKey(ks uint32) (string, rune, bool) {
	switch {
	case ks >= 0xffb0 && ks <= 0xffb9:
		return "", rune('0' + (ks - 0xffb0)), true
	case ks == 0xffab:
		return "", '+', true
	case ks == 0xffad:
		return "", '-', true
	case ks == 0xffaa:
		return "", '*', true
	case ks == 0xffaf:
		return "", '/', true
	case ks == 0xffac:
		return "", ',', true
	case ks == 0xff8d:
		return "Enter", 0, true
	case ks == 0xffae:
		return "", '.', true
	}
	return "", 0, false
}

func validRune(r rune) bool {
	return r >= 0x20 && r != 0x7f && r <= 0x10ffff && (r < 0xd800 || r > 0xdfff)
}

type VNCApp struct {
	Base
	d   *Desktop
	win *Window

	press    string
	lastCli  int
	lastRun  bool
	errLine  string
	btnPower ui.Rect
	btnCopy  ui.Rect
	btnClose ui.Rect
	addrR    ui.Rect
}

func (d *Desktop) openVNCServer() {
	for _, w := range d.windows {
		if _, ok := w.App.(*VNCApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &VNCApp{d: d}
	a.win = d.Open(a)
}

func (a *VNCApp) Title() string           { return "系统级 VNC 服务器" }
func (a *VNCApp) Icon() ui.IconKind       { return ui.IcoVNC }
func (a *VNCApp) DefaultSize() (int, int) { return 620, 470 }
func (a *VNCApp) MinSize() (int, int)     { return 620, 470 }
func (a *VNCApp) Resizable() bool         { return false }
func (a *VNCApp) Close()                  {}

func (a *VNCApp) ensureStarted() {
	srv := a.d.vncServer()
	if srv.Running() {
		return
	}
	if err := srv.Start(); err != nil {
		a.errLine = err.Error()
	} else {
		a.errLine = ""
	}
}

func (a *VNCApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	srv := d.vncServer()
	running := srv.Running()
	a.lastRun = running
	clients := srv.ClientCount()
	a.lastCli = clients

	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	ui.DrawIcon32(c, ui.IcoVNC, r.X+18, r.Y+16, false)
	f.TextBold(c, r.X+62, r.Y+18, "系统级 VNC 服务器", ui.ColText)
	state := "已停止"
	col := ui.ColRed
	if running {
		state = "正在运行"
		col = 0x008000
	}
	f.Text(c, r.X+62, r.Y+38, "服务器状态："+state, col)

	box := ui.Rect{r.X + 18, r.Y + 66, r.W - 36, 76}
	c.FillRect(box, ui.ColField)
	c.Sunken(box)
	addr := "未启动"
	if running {
		addr = srv.Address()
	}
	a.addrR = box
	f.Text(c, box.X+8, box.Y+8, "连接地址", ui.ColShadow)
	f.TextBold(c, box.X+8, box.Y+27, addr, ui.ColNavy)
	f.Text(c, box.X+8, box.Y+50, fmt.Sprintf("已连接客户端：%d", clients), ui.ColText)

	y := box.Bottom() + 12
	lines := []string{
		"Mac 连接方法：Finder → 前往 → 连接服务器…，输入 vnc://" + addr,
		"Windows / Linux 可用 TigerVNC、RealVNC 等客户端连接。",
		"VNC 会实时看到此桌面，并可以控制鼠标与键盘。",
	}
	if !running {
		lines = []string{
			"点击下方“启动服务器”，然后在另一台电脑上用 VNC 客户端连接。",
			"服务器监听 5900–5909 中第一个空闲端口。",
		}
	}
	for _, line := range lines {
		f.TextClip(c, r.X+18, y, r.W-36, line, ui.ColText)
		y += 20
	}
	f.TextClip(c, r.X+18, y+2, r.W-36, "安全提示：VNC 使用无密码连接，请只在可信的局域网内使用。", ui.ColRed)
	if a.errLine != "" {
		f.TextClip(c, r.X+18, y+24, r.W-36, a.errLine, ui.ColRed)
	}

	by := r.Bottom() - 36
	a.btnPower = ui.Rect{r.Right() - 314, by, 92, 26}
	a.btnCopy = ui.Rect{r.Right() - 216, by, 92, 26}
	a.btnClose = ui.Rect{r.Right() - 118, by, 92, 26}
	label := "启动服务器"
	if running {
		label = "停止服务器"
	}
	ui.Button(c, a.btnPower, label, a.press == "power", true)
	ui.Button(c, a.btnCopy, "复制地址", a.press == "copy", running)
	ui.Button(c, a.btnClose, "关闭", a.press == "close", true)
}

func (a *VNCApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnPower.Contains(ev.X, ev.Y):
			a.press = "power"
		case a.btnCopy.Contains(ev.X, ev.Y):
			a.press = "copy"
		case a.btnClose.Contains(ev.X, ev.Y):
			a.press = "close"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		srv := d.vncServer()
		switch p {
		case "power":
			if a.btnPower.Contains(ev.X, ev.Y) {
				if srv.Running() {
					srv.Stop()
					d.Notify("VNC 服务器", "VNC 服务器已停止")
				} else {
					a.ensureStarted()
					if srv.Running() {
						d.Notify("VNC 服务器", "正在监听 "+srv.Address())
					}
				}
				d.Invalidate()
			}
		case "copy":
			if a.btnCopy.Contains(ev.X, ev.Y) && srv.Running() {
				clipboardWrite(srv.Address())
				d.Notify("VNC 服务器", "地址已复制："+srv.Address())
			}
		case "close":
			if a.btnClose.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
	}
}

func (a *VNCApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Enter":
		srv := d.vncServer()
		if !srv.Running() {
			a.ensureStarted()
		}
		d.Invalidate()
		return true
	}
	if ev.Rune == 'v' && ev.Ctrl {
		clipboardWrite(d.vncServer().Address())
		d.Notify("VNC 服务器", "地址已复制")
		return true
	}
	return false
}

func (a *VNCApp) Tick(d *Desktop, w *Window, now time.Time) {
	srv := d.vncServer()
	if srv.Running() != a.lastRun || srv.ClientCount() != a.lastCli {
		a.lastRun = srv.Running()
		a.lastCli = srv.ClientCount()
		d.Invalidate()
	}
}
