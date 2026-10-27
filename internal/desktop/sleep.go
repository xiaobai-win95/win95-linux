package desktop

import (
	"errors"
	"time"

	"win95linux/internal/ui"
)

var errNoSleep = errors.New("当前环境不支持睡眠")

func (d *Desktop) showSleep() {
	if d.Mode == ModeInstaller {
		d.MessageBox("睡眠", "安装程序运行期间无法进入睡眠。", IconInfo, []string{"确定"}, nil)
		return
	}
	if d.Host.Suspend == nil {
		d.MessageBox("睡眠", "当前环境不支持睡眠。", IconWarning, []string{"确定"}, nil)
		return
	}
	d.MessageBox("睡眠", "计算机将进入睡眠状态，以节省电力。\n\n按任意键或移动鼠标即可唤醒。", IconQuestion,
		[]string{"睡眠", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			d.startOpen = false
			d.startSub = ""
			d.startSel = ""
			d.closeContext()
			d.closeMenusExcept(nil)
			d.sleepPending = true
			d.lastInput = time.Now()
			d.Invalidate()
		})
}

// runSleep is called from Animate on the frame after the sleep screen has
// been painted. Host.Suspend blocks until the machine wakes up.
func (d *Desktop) runSleep() {
	d.sleepPending = false
	d.sleeping = true
	d.Invalidate()
	var err error
	if d.Host.Suspend == nil {
		err = errNoSleep
	} else {
		err = d.Host.Suspend()
	}
	d.sleeping = false
	d.lastInput = time.Now()
	d.cpuTotal = 0
	d.Invalidate()
	if err != nil {
		d.MessageBox("睡眠", "无法进入睡眠：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	d.MessageBox("睡眠", "系统已从睡眠中唤醒。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) drawSleep(c *ui.Canvas) {
	c.FillRect(ui.Rect{0, 0, c.W, c.H}, 0x000030)
	f := ui.DefaultFont()
	title := "Windows 正在进入睡眠"
	tw := f.ScaledWidth(title, 2)
	f.Scaled(c, (c.W-tw)/2, c.H/2-70, title, ui.ColWhite, 2)

	hint := "按任意键或移动鼠标唤醒计算机"
	f.Text(c, (c.W-f.Width(hint))/2, c.H/2+10, hint, 0xA0C0FF)

	sub := "Windows 95 Linux"
	f.Text(c, (c.W-f.Width(sub))/2, c.H-60, sub, 0x7080A0)
}
