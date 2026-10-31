package desktop

import (
	"fmt"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const nukePassword = "0000"

func (d *Desktop) openDeadHand() {
	d.closeContext()
	if d.bsod || d.nuke {
		return
	}
	a := &NukeAuthApp{d: d}
	a.win = d.OpenModal(a)
}

// NukeAuthApp asks for the four digit launch code before arming the countdown.
type NukeAuthApp struct {
	Base
	d     *Desktop
	win   *Window
	code  []rune
	err   string
	press string

	fieldR, okR, cancelR ui.Rect
}

func (a *NukeAuthApp) Title() string           { return "死手系统 - 身份验证" }
func (a *NukeAuthApp) Icon() ui.IconKind       { return ui.IcoNuke }
func (a *NukeAuthApp) DefaultSize() (int, int) { return 500, 274 }
func (a *NukeAuthApp) MinSize() (int, int)     { return 500, 274 }
func (a *NukeAuthApp) Resizable() bool         { return false }

func (a *NukeAuthApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	head := ui.Rect{r.X, r.Y, r.W, 54}
	c.FillRect(head, 0x500000)
	c.HLine(head.X, head.Y, head.W, 0xA00000)
	f := ui.DefaultFont()
	f.Scaled(c, r.X+16, r.Y+9, "死手系统", ui.ColWhite, 2)
	f.Text(c, r.X+18, r.Y+39, "DEAD HAND · NUCLEAR RETALIATION CONTROL", 0xE0A0A0)

	f.Text(c, r.X+18, r.Y+68, "检测到指挥链中断。核反击系统已进入待授权状态。", ui.ColText)
	f.Text(c, r.X+18, r.Y+89, "请输入四位授权密码：", ui.ColText)

	a.fieldR = ui.Rect{r.X + 18, r.Y + 116, 180, 28}
	c.FillRect(a.fieldR, ui.ColBlack)
	c.Sunken(a.fieldR)
	masked := strings.Repeat("*", len(a.code))
	f.Text(c, a.fieldR.X+8, a.fieldR.Y+6, masked, 0x30FF30)
	cx := a.fieldR.X + 8 + f.Width(masked)
	if cx < a.fieldR.Right()-4 {
		c.VLine(cx, a.fieldR.Y+5, 18, 0x30FF30)
	}
	f.Text(c, a.fieldR.Right()+14, a.fieldR.Y+6, "仅限授权人员", ui.ColShadow)

	f.TextClip(c, r.X+18, r.Y+154, r.W-36, "警告：授权后将启动不可中断的模拟核反击倒计时。", 0xA00000)
	if a.err != "" {
		f.TextClip(c, r.X+18, r.Y+176, r.W-36, a.err, ui.ColRed)
	} else {
		f.Text(c, r.X+18, r.Y+176, "提示：在倒计时结束前仍可取消。", ui.ColShadow)
	}

	a.okR = ui.Rect{r.Right() - 196, r.Bottom() - 42, 84, 26}
	a.cancelR = ui.Rect{r.Right() - 104, r.Bottom() - 42, 84, 26}
	ui.Button(c, a.okR, "授权", a.press == "ok", true)
	ui.Button(c, a.cancelR, "取消", a.press == "cancel", true)
}

func (a *NukeAuthApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.okR.Contains(ev.X, ev.Y) {
			a.press = "ok"
		} else if a.cancelR.Contains(ev.X, ev.Y) {
			a.press = "cancel"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "ok":
			if a.okR.Contains(ev.X, ev.Y) {
				a.accept()
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) {
				a.finish()
			}
		}
	}
}

func (a *NukeAuthApp) accept() {
	if string(a.code) != nukePassword {
		a.err = "授权密码错误。核反击控制保持锁定。"
		beep()
		a.d.Invalidate()
		return
	}
	a.finish()
	a.d.openNukeCountdown()
}

func (a *NukeAuthApp) finish() {
	if a.win != nil {
		a.d.CloseWindowForced(a.win)
	}
}

func (a *NukeAuthApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		a.finish()
		return true
	case "Enter":
		a.accept()
		return true
	case "Backspace":
		if len(a.code) > 0 {
			a.code = a.code[:len(a.code)-1]
			a.err = ""
		}
		return true
	}
	if ev.Rune >= '0' && ev.Rune <= '9' && len(a.code) < 4 {
		a.code = append(a.code, ev.Rune)
		a.err = ""
		return true
	}
	return false
}

func (a *NukeAuthApp) Close() {}

func (d *Desktop) openNukeCountdown() {
	a := &NukeCountdownApp{d: d, deadline: time.Now().Add(10 * time.Second), lastTick: -1}
	a.win = d.OpenModal(a)
	beep()
}

// NukeCountdownApp shows the retro launch countdown. Cancelling disarms it;
// otherwise the desktop flashes red and raises the custom blue screen.
type NukeCountdownApp struct {
	Base
	d        *Desktop
	win      *Window
	deadline time.Time
	lastTick int
	press    string

	cancelR ui.Rect
}

func (a *NukeCountdownApp) Title() string           { return "死手系统 - 发射倒计时" }
func (a *NukeCountdownApp) Icon() ui.IconKind       { return ui.IcoNuke }
func (a *NukeCountdownApp) DefaultSize() (int, int) { return 600, 380 }
func (a *NukeCountdownApp) MinSize() (int, int)     { return 600, 380 }
func (a *NukeCountdownApp) Resizable() bool         { return false }

func (a *NukeCountdownApp) remaining() int {
	left := int(time.Until(a.deadline).Seconds() + 0.999)
	if left < 0 {
		left = 0
	}
	return left
}

func (a *NukeCountdownApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	left := a.remaining()
	c.FillRect(r, ui.ColBlack)
	head := ui.Rect{r.X, r.Y, r.W, 38}
	c.FillRect(head, 0x500000)
	c.HLine(head.X, head.Bottom()-1, head.W, 0xC00000)
	f := ui.DefaultFont()
	f.TextBold(c, r.X+12, r.Y+10, "死手系统 · 核反击已授权", ui.ColWhite)

	pulse := uint32(0x800000)
	if left%2 == 0 {
		pulse = 0xB00000
	}
	box := ui.Rect{r.X + 14, r.Y + 52, r.W - 28, 190}
	c.FillRect(box, ui.ColBlack)
	c.Frame(box, pulse)
	digits := fmt.Sprintf("%02d", left)
	scale := 6
	tw := f.ScaledWidth(digits, scale)
	f.Scaled(c, r.X+(r.W-tw)/2, box.Y+20, digits, 0xFF3030, scale)
	f.Text(c, r.X+(r.W-f.Width("SECONDS TO LAUNCH"))/2, box.Y+125, "SECONDS TO LAUNCH", 0xFFB0B0)
	f.Text(c, r.X+(r.W-f.Width("目标：模拟演习场 · 指令已锁定"))/2, box.Y+150, "目标：模拟演习场 · 指令已锁定", 0xE0C0C0)

	f.TextClip(c, r.X+18, r.Y+262, r.W-36, "核反击将在倒计时结束后执行。按“取消发射”可中止。", 0xFF9090)
	a.cancelR = ui.Rect{r.X + (r.W-140)/2, r.Bottom() - 52, 140, 30}
	ui.Button(c, a.cancelR, "取消发射", a.press == "cancel", true)
}

func (a *NukeCountdownApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.cancelR.Contains(ev.X, ev.Y) {
			a.press = "cancel"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		if p == "cancel" && a.cancelR.Contains(ev.X, ev.Y) {
			a.abort()
		}
	}
}

func (a *NukeCountdownApp) abort() {
	if a.win != nil {
		a.d.CloseWindowForced(a.win)
	}
	a.d.Notify("死手系统", "核反击已中止，系统恢复常规状态。")
}

func (a *NukeCountdownApp) Tick(d *Desktop, w *Window, now time.Time) {
	left := a.remaining()
	if left == 0 {
		d.CloseWindowForced(w)
		d.triggerNuke()
		return
	}
	if left <= 3 && left != a.lastTick {
		a.lastTick = left
		beep()
	}
	d.Invalidate()
}

func (a *NukeCountdownApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" {
		a.abort()
		return true
	}
	return false
}

func (a *NukeCountdownApp) Close() {}

func (d *Desktop) triggerNuke() {
	d.nuke = true
	d.nukeAt = time.Now()
	d.nukeStep = d.nukeAt
	d.startOpen = false
	d.closeMenusExcept(nil)
	beep()
	d.Invalidate()
}

func (d *Desktop) drawNuke(c *ui.Canvas) {
	elapsed := time.Since(d.nukeAt)
	flash := int(elapsed.Milliseconds()/90)%2 == 0
	bg := uint32(0x900000)
	if flash {
		bg = 0xC00000
	}
	c.FillRect(ui.Rect{0, 0, c.W, c.H}, bg)
	for y := 0; y < c.H; y += 4 {
		c.HLine(0, y, c.W, 0x600000)
	}
	f := ui.DefaultFont()
	title := "SYSTEM OVERRIDE"
	tw := f.ScaledWidth(title, 4)
	f.Scaled(c, (c.W-tw)/2, c.H/2-130, title, ui.ColWhite, 4)
	sub := "NUCLEAR STRIKE CONFIRMED"
	sw := f.ScaledWidth(sub, 2)
	f.Scaled(c, (c.W-sw)/2, c.H/2-30, sub, 0xFFE000, 2)
	msg := "死手系统已接管本机"
	f.Text(c, (c.W-f.Width(msg))/2, c.H/2+40, msg, ui.ColWhite)
	msg2 := "这只是彩蛋，系统数据没有丢失"
	f.Text(c, (c.W-f.Width(msg2))/2, c.H/2+66, msg2, 0xFFD0D0)
}
