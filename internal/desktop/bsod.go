package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

const bsodGrace = 900 * time.Millisecond

func (d *Desktop) showBSOD() {
	d.showBSODText("")
}

func (d *Desktop) showBSODText(text string) {
	d.bsod = true
	d.bsodText = text
	d.bsodAt = time.Now()
	d.bsodStep = d.bsodAt
	d.startOpen = false
	d.startSub = ""
	d.closeMenusExcept(nil)
	beep()
	d.Invalidate()
}

// handleBSODInput implements "press any key to continue". The short grace
// period prevents the click that opened the blue screen from closing it.
func (d *Desktop) handleBSODInput() {
	if time.Since(d.bsodAt) < bsodGrace {
		return
	}
	d.bsod = false
	d.bsodText = ""
	d.lastInput = time.Now()
	d.Invalidate()
}

func (d *Desktop) drawBSOD(c *ui.Canvas) {
	const blue = 0x0000AA
	c.FillRect(ui.Rect{0, 0, c.W, c.H}, blue)
	f := ui.DefaultFont()

	title := "Windows"
	tw := f.ScaledWidth(title, 3)
	f.Scaled(c, (c.W-tw)/2, 70, title, ui.ColWhite, 3)

	nuke := d.bsodText != ""
	if nuke {
		scale := 2
		for scale > 1 && f.ScaledWidth(d.bsodText, scale) > c.W-120 {
			scale--
		}
		tw = f.ScaledWidth(d.bsodText, scale)
		f.Scaled(c, (c.W-tw)/2, 128, d.bsodText, 0xFFD700, scale)
	}

	lines := []string{
		"",
		"         A fatal exception 0E has occurred at 0028:C0011E36 in VXD",
		"         VMM(01) + 00010E36. The current application will be",
		"         terminated.",
		"",
		"         *  Press any key to terminate the current application.",
		"         *  Press CTRL+ALT+DEL again to restart your computer. You",
		"            will lose any unsaved information in all applications.",
		"",
		"         EAX=00000000  EBX=C0011E36  ECX=0000000E  EDX=0000C001",
		"         ESI=81C3A000  EDI=00000000  EIP=C0011E36  ESP=81C3FF00",
		"         win95::panic(USER_FAULT, rip=0x0028C0011E36, cr2=0xDEADBEEF)",
		"",
	}
	if nuke {
		lines = append(lines,
			"         死手系统已执行核反击指令。这只是彩蛋，数据没有丢失。",
		)
	} else {
		lines = append(lines,
			"         系统已停止响应。这只是“危险操作”彩蛋，数据没有丢失。",
		)
	}
	lines = append(lines, "")
	startY := 178
	if nuke {
		startY = 220
	}
	y := startY
	for _, line := range lines {
		f.Text(c, 96, y, line, ui.ColWhite)
		y += 22
	}

	blink := "Press any key to continue _"
	if time.Since(d.bsodAt).Milliseconds()/500%2 == 1 {
		blink = "Press any key to continue"
	}
	f.Text(c, (c.W-f.Width(blink))/2, c.H-90, blink, ui.ColWhite)

	hint := "按任意键返回 Windows 95"
	f.Text(c, (c.W-f.Width(hint))/2, c.H-50, hint, 0xA0C0FF)

	// A fake stack dump along the bottom sells the illusion.
	for i := 0; i < 6; i++ {
		line := fmt.Sprintf("%04X: %08X %08X %08X %08X", 0xA000+i*16,
			0xC0011E36+i, 0xDEADBEEF-i, 0x00000000+i, 0x81C3FF00-i)
		f.Text(c, 96, c.H-210+i*16, line, 0x8090C0)
	}
}
