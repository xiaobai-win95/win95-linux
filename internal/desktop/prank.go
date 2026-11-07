package desktop

import (
	"fmt"
	"math"
	"sort"
	"time"

	"win95linux/internal/ui"
)

const (
	prankStageWarning = iota
	prankStageFormat
	prankStageDance
	prankStageStripes
	prankStageShapes
	prankStageMessage
	prankStageVortex
	prankStageChaos
	prankStageBSOD
)

const (
	prankFormatDur  = 8 * time.Second
	prankDanceDur   = 3 * time.Second
	prankStripesDur = 3 * time.Second
	prankShapesDur  = 3 * time.Second
	prankMessageDur = 2600 * time.Millisecond
	prankVortexDur  = 4200 * time.Millisecond
	prankChaosDur   = 3200 * time.Millisecond
	prankChaosMove  = 2200 * time.Millisecond
	prankBSODDur    = 3200 * time.Millisecond
)

const prankBanner = "仅视觉演示！！不破坏系统 · 虚构病毒动画，不接触磁盘或分区表"

// prankFloat is one icon or window caught by the fake vortex.
type prankFloat struct {
	label string
	icon  ui.IconKind
	virus bool
	win   bool
	w, h  int

	ang   float64
	rad   float64
	spin  float64
	phase float64
}

// prankPopup is a fake terminal or error dialog that pops up over the overlay
// and closes itself. It is pure drawing: no real window is created.
type prankPopup struct {
	x, y, w, h int
	born       time.Time
	life       time.Duration
	terminal   bool
	seed       uint32
}

// prankDemo is a harmless visual joke that overlays the live desktop. The real
// desktop keeps running underneath: input is never blocked, and clicking or
// pressing a key briefly reveals the real desktop before the overlay re-covers
// it. Once confirmed it cannot be aborted mid-run; it ends by itself. It never
// opens a device, writes a file or touches a partition table.
type prankDemo struct {
	stage   int
	gentle  bool
	stageAt time.Time
	runAt   time.Time
	press   string

	snap  *ui.Canvas
	items []prankFloat

	revealUntil time.Time
	popups      []prankPopup
	popupSeq    uint32
	nextPopupAt time.Time

	normalR, gentleR, cancelR ui.Rect
}

func newPrankDemo(now time.Time) *prankDemo {
	return &prankDemo{stage: prankStageWarning, stageAt: now}
}

func (d *Desktop) openPrankDemo() {
	if d.prank != nil {
		return
	}
	d.startOpen = false
	d.startSub = ""
	d.closeMenusExcept(nil)
	d.ctx = nil
	d.lastInput = time.Now()
	d.prank = newPrankDemo(time.Now())
	d.Invalidate()
}

func (p *prankDemo) begin(gentle bool) {
	p.gentle = gentle
	p.stage = prankStageFormat
	now := time.Now()
	p.stageAt = now
	p.runAt = now
	p.press = ""
	p.snap = nil
	p.items = nil
	p.popups = nil
	p.popupSeq = 0
	p.nextPopupAt = time.Time{}
	p.revealUntil = time.Time{}
}

// noteInput briefly reveals the real desktop so windows, menus and clicks
// surface above the overlay before it covers them again.
func (p *prankDemo) noteInput() {
	p.revealUntil = time.Now().Add(600 * time.Millisecond)
}

func (p *prankDemo) enter(d *Desktop, stage int, now time.Time) {
	p.stage = stage
	p.stageAt = now
	p.press = ""
	switch stage {
	case prankStageDance, prankStageVortex, prankStageChaos, prankStageBSOD:
		beep()
	}
	if stage == prankStageVortex {
		p.gather(d)
	}
	if stage == prankStageDance {
		p.nextPopupAt = now.Add(500 * time.Millisecond)
	}
	d.Invalidate()
}

func (p *prankDemo) animate(d *Desktop, now time.Time) {
	switch p.stage {
	case prankStageWarning:
		return
	case prankStageFormat:
		if now.Sub(p.stageAt) >= prankFormatDur {
			p.enter(d, prankStageDance, now)
		}
	case prankStageDance:
		if now.Sub(p.stageAt) >= prankDanceDur {
			p.enter(d, prankStageStripes, now)
		}
	case prankStageStripes:
		if now.Sub(p.stageAt) >= prankStripesDur {
			p.enter(d, prankStageShapes, now)
		}
	case prankStageShapes:
		if now.Sub(p.stageAt) >= prankShapesDur {
			p.enter(d, prankStageMessage, now)
		}
	case prankStageMessage:
		if now.Sub(p.stageAt) >= prankMessageDur {
			p.enter(d, prankStageVortex, now)
		}
	case prankStageVortex:
		if now.Sub(p.stageAt) >= prankVortexDur {
			p.enter(d, prankStageChaos, now)
		}
	case prankStageChaos:
		if now.Sub(p.stageAt) >= prankChaosDur {
			p.enter(d, prankStageBSOD, now)
		}
	case prankStageBSOD:
		if now.Sub(p.stageAt) >= prankBSODDur {
			p.finish(d)
			return
		}
	}
	p.updatePopups(d, now)
	d.Invalidate()
}

func (p *prankDemo) finish(d *Desktop) {
	if d.prank == nil {
		return
	}
	d.prank = nil
	d.Invalidate()
	d.MessageBox("视觉整活演示",
		"演示结束。\n\n"+
			"刚才的“格式化”“苏尔科病毒”六阶段动画、反色、乱弹窗和蓝屏全部是内存中的模拟画面：\n"+
			"· 没有格式化任何磁盘\n"+
			"· 没有修改分区表\n"+
			"· 没有删除或移动任何文件\n\n"+
			"六个阶段：桌面跳舞 → 绿色横纹 → 形状爆炸 → 中心宣告 → 吞噬漩涡 → 图标大乱斗 + 假蓝屏。\n"+
			"正常模式含每秒 1 次全屏反色与快速闪烁；如果感到不适，\n下次请选择温和模式（无闪烁、无反色）。",
		IconInfo, []string{"确定"}, nil)
}

func (p *prankDemo) abort(d *Desktop) {
	if d.prank == nil {
		return
	}
	d.prank = nil
	d.Invalidate()
	d.MessageBox("视觉整活演示",
		"已取消演示。\n\n没有写入任何磁盘，也没有修改分区表或删除任何文件。",
		IconInfo, []string{"确定"}, nil)
}

// mouse only handles the pre-run confirmation screen. While the demo runs,
// input is routed to the real desktop by events.go instead.
func (p *prankDemo) mouse(d *Desktop, ev MouseEvent) {
	if ev.Button != 1 || p.stage != prankStageWarning {
		return
	}
	switch ev.Kind {
	case MouseDown:
		p.press = ""
		switch {
		case p.normalR.Contains(ev.X, ev.Y):
			p.press = "normal"
		case p.gentleR.Contains(ev.X, ev.Y):
			p.press = "gentle"
		case p.cancelR.Contains(ev.X, ev.Y):
			p.press = "cancel"
		}
	case MouseUp:
		press := p.press
		p.press = ""
		switch press {
		case "normal":
			if p.normalR.Contains(ev.X, ev.Y) {
				p.begin(false)
			}
		case "gentle":
			if p.gentleR.Contains(ev.X, ev.Y) {
				p.begin(true)
			}
		case "cancel":
			if p.cancelR.Contains(ev.X, ev.Y) {
				p.abort(d)
			}
		}
		if d.prank != nil {
			d.Invalidate()
		}
	}
}

func (p *prankDemo) key(d *Desktop, ev KeyEvent) {
	if p.stage != prankStageWarning {
		return
	}
	if ev.Name == "Esc" {
		p.abort(d)
		return
	}
	if ev.Name == "1" || ev.Rune == '1' {
		p.begin(false)
		d.Invalidate()
	} else if ev.Name == "2" || ev.Rune == '2' {
		p.begin(true)
		d.Invalidate()
	}
}

// draw composites the overlay on top of the live desktop. The real scene is
// drawn first (so input targets stay alive), then the fake virus effect, then
// the self-closing popups, the safety banner and finally the cursor.
func (p *prankDemo) draw(d *Desktop, c *ui.Canvas) {
	if p.stage == prankStageWarning {
		p.drawWarning(c)
		return
	}
	now := time.Now()
	if now.Before(p.revealUntil) {
		d.drawScene(c, true)
		prankDisclaimer(c)
		return
	}
	d.drawScene(c, false)
	switch p.stage {
	case prankStageFormat:
		p.drawFormat(c)
	case prankStageDance:
		p.drawDance(d, c)
	case prankStageStripes:
		p.drawStripes(d, c)
	case prankStageShapes:
		p.drawShapes(d, c)
	case prankStageMessage:
		p.drawMessage(d, c)
	case prankStageVortex:
		p.drawVortex(c)
	case prankStageChaos:
		p.drawChaos(c)
	case prankStageBSOD:
		p.drawBSOD(c)
	}
	if p.invertActive(now) {
		prankInvert(c)
	}
	p.drawPopups(c, now)
	p.drawStageChrome(c, p.stageHint())
	if d.hand {
		ui.CursorHand(c, d.MouseX, d.MouseY)
	} else {
		ui.Cursor(c, d.MouseX, d.MouseY)
	}
}

func (p *prankDemo) stageHint() string {
	switch p.stage {
	case prankStageFormat:
		return "仅视觉演示 · 没有调用 mkfs / fdisk / dd · 没有访问 /dev"
	case prankStageDance:
		return "第一阶段：桌面跳舞 · 点击或按键可短暂显现真实桌面，随后又被覆盖"
	case prankStageStripes:
		return "第二阶段：绿色横纹 · 信号故障（模拟）"
	case prankStageShapes:
		return "第三阶段：形状爆炸 · 桌面仍可操作，特效会重新覆盖"
	case prankStageMessage:
		return "第四阶段：中心宣告 · 虚构病毒的名字只是动画台词"
	case prankStageVortex:
		return "第五阶段：吞噬漩涡 · 所有图标都被吸向中心（模拟）"
	case prankStageChaos:
		if p.elapsed() >= prankChaosMove {
			return "画面已卡死 · 即将出现假蓝屏（模拟，没有文件受影响）"
		}
		return "第六阶段：图标大乱斗 · 系统即将崩溃（模拟）"
	case prankStageBSOD:
		return "假蓝屏 · 仅视觉演示，没有发生任何真实的磁盘操作"
	}
	return ""
}

// invertActive flips the whole screen once per second in normal mode during
// the chaotic stages. Gentle mode never inverts.
func (p *prankDemo) invertActive(now time.Time) bool {
	if p.gentle {
		return false
	}
	switch p.stage {
	case prankStageDance, prankStageStripes, prankStageShapes, prankStageMessage, prankStageVortex, prankStageChaos:
		return int(now.Sub(p.runAt)/time.Second)%2 == 0
	}
	return false
}

func (p *prankDemo) elapsed() time.Duration {
	e := time.Since(p.stageAt)
	if e < 0 {
		e = 0
	}
	return e
}

func prankFrac(d, total time.Duration) float64 {
	if total <= 0 {
		return 1
	}
	f := float64(d) / float64(total)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f
}

// snapDesktop captures the normal desktop once, without cursor or prank
// overlay, for the screen-wide transformations.
func (p *prankDemo) snapDesktop(d *Desktop) *ui.Canvas {
	if p.snap == nil || p.snap.W != d.W || p.snap.H != d.H {
		p.snap = ui.NewCanvas(d.W, d.H)
		d.drawScene(p.snap, false)
	}
	return p.snap
}

func prankDisclaimer(c *ui.Canvas) {
	f := ui.DefaultFont()
	c.FillRect(ui.Rect{0, 0, c.W, 28}, 0xFFD000)
	c.HLine(0, 27, c.W, 0x806000)
	tw := f.Width(prankBanner)
	if tw > c.W-20 {
		tw = c.W - 20
	}
	x := (c.W - tw) / 2
	if x < 10 {
		x = 10
	}
	f.TextClip(c, x, 6, tw, prankBanner, 0x000000)
}

// drawStageChrome keeps the harmless-demo banner visible on every animated
// stage. There is no abort button: once confirmed the demo runs to the end.
func (p *prankDemo) drawStageChrome(c *ui.Canvas, hint string) {
	f := ui.DefaultFont()
	c.FillRect(ui.Rect{0, 0, c.W, 28}, 0xFFD000)
	c.HLine(0, 27, c.W, 0x806000)
	f.TextClip(c, 8, 6, c.W-280, prankBanner, 0x000000)
	status := "演示进行中 · 无法中途退出 · 自动结束"
	f.TextRight(c, c.W-10, 6, status, 0x701010)
	if hint != "" {
		tw := f.Width(hint)
		x := (c.W - tw) / 2
		if x < 8 {
			x = 8
		}
		y := c.H - 60
		c.FillRect(ui.Rect{x - 5, y - 3, tw + 10, 21}, 0x000000)
		f.TextClip(c, x, y, c.W-16, hint, 0xFFE060)
	}
}

func (p *prankDemo) drawWarning(c *ui.Canvas) {
	W, H := c.W, c.H
	f := ui.DefaultFont()
	c.FillRect(ui.Rect{0, 0, W, H}, 0x0E1620)
	for y := 28; y < H; y += 40 {
		c.HLine(0, y, W, 0x121E2A)
	}
	prankDisclaimer(c)

	titleY := 48
	if H >= 700 {
		titleY = 54
	}
	f.Scaled(c, 40, titleY, "视觉整活演示 95 · 苏尔科篇", 0xFFFFFF, 2)
	f.Text(c, 42, titleY+40, "一次假的磁盘格式化 + 六阶段虚构病毒动画——真的不会碰你的磁盘。", 0xA0C0D8)

	warn := ui.Rect{40, titleY + 68, W - 80, 216}
	c.FillRect(warn, 0x2A1000)
	c.Frame(warn, 0xE0A000)
	f.TextBold(c, warn.X+16, warn.Y+10, "光敏性癫痫警告 · 精神污染画面", 0xFFC040)
	warnLines := []string{
		"· 本演示包含闪烁、全屏反色、横向撕裂、抖动、高对比色块与快速旋转。",
		"· 极少数人在观看闪烁画面时可能出现光敏性癫痫发作。",
		"· 如您或家人有光敏性癫痫病史，请勿观看正常模式，可选择「温和模式」。",
		"· 正常模式含每秒 1 次全屏反色与快速闪烁；温和模式无闪烁、无反色。",
		"· 确认后约 30 秒内无法中途退出，只能等待自动结束，请谨慎开始。",
		"· 观看时如出现头晕、眼花或不适，请立即闭眼并离开屏幕。",
		"· 六阶段：桌面跳舞、绿色横纹、形状爆炸、中心宣告、漩涡、图标大乱斗 + 假蓝屏。",
	}
	for i, line := range warnLines {
		f.TextClip(c, warn.X+16, warn.Y+38+i*21, warn.W-32, line, 0xE8D0B0)
	}

	safe := ui.Rect{40, warn.Bottom() + 14, W - 80, 116}
	c.FillRect(safe, 0x07240F)
	c.Frame(safe, 0x40C060)
	f.TextBold(c, safe.X+16, safe.Y+8, "仅视觉演示！！不破坏系统", 0x80FF80)
	safeLines := []string{
		"· 只绘制内存中的动画，不调用 mkfs、fdisk、dd 等任何磁盘工具。",
		"· 不打开 /dev 设备，不写入分区表，不删除或移动任何文件。",
		"· 特效覆盖在桌面之上，但桌面仍可操作；结束后一切保持原样。",
	}
	for i, line := range safeLines {
		f.TextClip(c, safe.X+16, safe.Y+32+i*21, safe.W-32, line, 0xB8E8C0)
	}

	buttonY := safe.Bottom() + 18
	if buttonY+84 > H {
		buttonY = H - 84
	}
	gap := 12
	total := 180*2 + 110 + gap*2
	x0 := (W - total) / 2
	if x0 < 20 {
		x0 = 20
	}
	p.normalR = ui.Rect{x0, buttonY, 180, 34}
	p.gentleR = ui.Rect{x0 + 180 + gap, buttonY, 180, 34}
	p.cancelR = ui.Rect{x0 + 180*2 + gap*2, buttonY, 110, 34}
	ui.Button(c, p.normalR, "正常开始 (1)", p.press == "normal", true)
	ui.Button(c, p.gentleR, "温和模式 (2)", p.press == "gentle", true)
	ui.Button(c, p.cancelR, "取消", p.press == "cancel", true)

	hint := "1 = 正常开始（含反色与闪烁） · 2 = 温和模式（无闪烁） · Esc = 取消"
	f.TextClip(c, (W-f.Width(hint))/2, buttonY+44, W-20, hint, 0x8FA6B8)
	f.TextClip(c, (W-f.Width("请根据自身情况选择演示模式，开始后将无法中途退出。"))/2, buttonY+66,
		W-20, "请根据自身情况选择演示模式，开始后将无法中途退出。", 0x607890)

	if H >= 700 {
		flowY := buttonY + 104
		labels := []string{"桌面跳舞", "绿色横纹", "形状爆炸", "中心宣告", "吞噬漩涡", "图标大乱斗"}
		bw, gap := 140, 10
		totalW := len(labels)*bw + (len(labels)-1)*gap
		x := (W - totalW) / 2
		heading := "演示流程（全部为内存中的虚构动画）"
		f.Text(c, (W-f.Width(heading))/2, flowY, heading, 0x8FA6B8)
		y := flowY + 24
		for i, label := range labels {
			r := ui.Rect{x + i*(bw+gap), y, bw, 30}
			c.FillRect(r, 0x142430)
			c.Frame(r, 0x2A4050)
			f.Text(c, r.X+8, r.Y+7, fmt.Sprintf("%d", i+1), 0xFFD000)
			f.Text(c, r.X+24, r.Y+7, label, 0xE0E8F0)
		}
	}
}

func (p *prankDemo) drawFormat(c *ui.Canvas) {
	W, H := c.W, c.H
	f := ui.DefaultFont()
	elapsed := p.elapsed()
	c.FillRect(ui.Rect{0, 0, W, H}, 0x000000)
	for y := 28; y < H; y += 4 {
		c.HLine(0, y, W, 0x060606)
	}

	head := ui.Rect{0, 28, W, 52}
	c.FillRect(head, 0x701010)
	c.HLine(0, head.Bottom()-1, W, 0xB02020)
	pct := int(float64(elapsed) / float64(prankFormatDur) * 100)
	if pct > 99 {
		pct = 99
	}
	if pct < 0 {
		pct = 0
	}
	title := "正在格式化 C: (NTFS)（模拟）"
	if pct >= 55 {
		title = "正在格式化 C: (NTFS) · 注入 SULKO.SYS（模拟）"
	}
	f.TextBold(c, 24, head.Y+14, title, 0xFFFFFF)
	dot := uint32(0x802020)
	if !p.gentle && int(elapsed/(600*time.Millisecond))%2 == 0 {
		dot = 0xFF4040
	}
	c.FillRect(ui.Rect{W - 60, head.Y + 18, 12, 12}, dot)
	f.TextRight(c, W-80, head.Y+16, "演示动画", 0xE0A0A0)

	mode := "正常模式"
	if p.gentle {
		mode = "温和模式"
	}
	f.Text(c, 24, 94, "驱动器 C:   ·   NTFS   ·   63.9 GB   ·   簇大小 4 KB", 0xC8C8C8)
	f.TextRight(c, W-24, 94, mode, 0x808080)

	bar := ui.Rect{24, 120, W - 48, 42}
	c.FillRect(bar, 0x181818)
	c.Sunken(bar)
	inner := ui.Rect{bar.X + 3, bar.Y + 3, bar.W - 6, bar.H - 6}
	c.FillRect(inner, 0x102040)
	fill := inner.W * pct / 100
	if fill > 0 {
		c.FillRect(ui.Rect{inner.X, inner.Y, fill, inner.H}, 0x2050C0)
		for x := inner.X + 8; x < inner.X+fill; x += 12 {
			c.VLine(x, inner.Y+1, inner.H-2, 0x3A70E0)
		}
	}
	label := fmt.Sprintf("正在格式化… %d%%", pct)
	f.TextBold(c, bar.X+(bar.W-f.Width(label))/2, bar.Y+13, label, 0xFFFFFF)

	f.Text(c, 24, 174, fmt.Sprintf("已用时间 %02d 秒 · 正在写入文件系统元数据（模拟）", int(elapsed.Seconds())), 0x9CCFFF)

	logBox := ui.Rect{24, 200, W - 48, H - 200 - 74}
	if logBox.H < 80 {
		logBox.H = 80
	}
	c.FillRect(logBox, 0x020A04)
	c.Frame(logBox, 0x205030)
	f.TextClip(c, logBox.X+10, logBox.Y+7, logBox.W-20, "低层格式化日志（全部为模拟数据，未接触真实磁盘）", 0x60A070)
	step := int(elapsed/(500*time.Millisecond)) + 1
	first := step - 11
	if first < 0 {
		first = 0
	}
	for i := first; i < step; i++ {
		line := prankLogLine(i)
		col := uint32(0x80FF80)
		if i == step-1 {
			col = 0xC8FFC8
		}
		y := logBox.Y + 28 + (i-first)*17
		if y+12 > logBox.Bottom() {
			break
		}
		f.TextClip(c, logBox.X+10, y, logBox.W-20, line, col)
	}
	if pct > 65 {
		alert := "> 警告：引导扇区校验失败（模拟）……"
		if pct > 85 {
			alert = "> 严重：检测到未知驱动 SULKO.SYS 正在加载（模拟）……"
		}
		f.TextClip(c, logBox.X+10, logBox.Bottom()-22, logBox.W-20, alert, 0xFF7070)
	}
}

func prankLogLine(i int) string {
	switch i {
	case 0:
		return "> 正在初始化低级格式化引擎（模拟）..."
	case 1:
		return "> 卷标: WIN95_DEMO    文件系统: NTFS    模式: 演示"
	case 2:
		return "> 读取 MBR 扇区 0x00000000 ... OK（演示）"
	case 3:
		return "> 校验分区表签名 55 AA ... OK（演示，未写入）"
	case 4:
		return "> 扫描坏道：0x00000000 - 0x00100000 ... 0 个（模拟）"
	case 5:
		return "> 初始化 $MFT 元数据 ... 完成（模拟）"
	case 6:
		return "> 分配簇位图 ... 完成（模拟）"
	case 7:
		return "> 写入文件系统引导记录 ... 完成（模拟）"
	case 8:
		return "> 写入可疑驱动 SULKO.SYS ... 0x40000 字节（模拟，仅内存）"
	case 9:
		return "> 注册启动项 HKCU\\...\\Run\\SULKO ... OK（模拟注册表）"
	case 10:
		return "> 启动六阶段视觉序列 ... 准备桌面跳舞（演示）"
	default:
		h := prankHash(uint32(i))
		return fmt.Sprintf("> 格式化簇 0x%06X ... 0x%02X%% 完成（演示）", h&0xFFFFFF, 10+int(h>>24)%89)
	}
}

func (p *prankDemo) drawDance(d *Desktop, c *ui.Canvas) {
	snap := p.snapDesktop(d)
	W, H := c.W, c.H
	c.FillRect(ui.Rect{0, 0, W, H}, 0x000000)
	t := prankFrac(p.elapsed(), prankDanceDur)
	var dx, dy int
	if t < 0.45 {
		u := t / 0.45
		dy = int(float64(H) * (1 - u))
		dy -= int(math.Abs(math.Sin(u*math.Pi*5)) * 48)
	} else {
		u := (t - 0.45) / 0.55
		dx = int(float64(W)/4 - float64(W)*0.9*u)
		dy = int(math.Abs(math.Sin(t*math.Pi*7))*10) - 10
	}
	prankBlitWrap(c, snap, dx, dy)
}

func (p *prankDemo) drawStripes(d *Desktop, c *ui.Canvas) {
	snap := p.snapDesktop(d)
	W, H := c.W, c.H
	t := prankFrac(p.elapsed(), prankStripesDur)
	c.FillRect(ui.Rect{0, 0, W, H}, 0x000000)
	dx := int(-float64(W)*0.25 - float64(W)*0.5*t)
	dy := int(math.Sin(float64(p.elapsed().Milliseconds())/140.0)*8) - 8
	prankBlitWrap(c, snap, dx, dy)
	p.greenStripes(c)
}

func (p *prankDemo) greenStripes(c *ui.Canvas) {
	tick := int(p.elapsed() / (520 * time.Millisecond))
	alphaScale := 1.0
	if p.gentle {
		tick = int(p.elapsed() / (1500 * time.Millisecond))
		alphaScale = 0.45
	}
	prankMixRect(c, ui.Rect{0, 0, c.W, c.H}, 0x00FF60, int(16*alphaScale))
	for i := 0; i < 22; i++ {
		h := prankHash(uint32(i+7), uint32(tick))
		y := int(h % uint32(c.H))
		bh := 1 + int((h>>8)%7)
		if p.gentle {
			bh = 1 + int((h>>8)%3)
		}
		col := uint32(0x00FF60)
		switch i % 3 {
		case 1:
			col = 0x00C040
		case 2:
			col = 0x80FFA0
		}
		a := int((0.30 + float64((h>>12)%34)/100) * alphaScale * 256)
		if i < 6 {
			bh += 6
			a += int(60 * alphaScale)
		}
		prankMixRect(c, ui.Rect{0, y, c.W, bh}, col, a)
	}
	for y := 0; y < c.H; y += 3 {
		prankMixRow(c, y, 0x00FF40, 18)
	}
	blocks := 7
	if p.gentle {
		blocks = 3
	}
	for i := 0; i < blocks; i++ {
		h := prankHash(uint32(i+77), uint32(tick))
		x := int(h % uint32(c.W))
		y := int((h >> 10) % uint32(c.H))
		w := 40 + int((h>>16)%uint32(c.W/2))
		bh := 2 + int((h>>5)%14)
		prankMixRect(c, ui.Rect{x, y, w, bh}, 0x00FF80, int(90*alphaScale))
	}
}

func (p *prankDemo) drawShapes(d *Desktop, c *ui.Canvas) {
	snap := p.snapDesktop(d)
	W, H := c.W, c.H
	c.FillRect(ui.Rect{0, 0, W, H}, 0x000000)
	t := prankFrac(p.elapsed(), prankShapesDur)
	prankBlitZoom(c, snap, 1+t*0.8)
	prankDim(c, 0.18)
	p.shapeBurst(c, t)
}

func (p *prankDemo) drawMessage(d *Desktop, c *ui.Canvas) {
	snap := p.snapDesktop(d)
	W, H := c.W, c.H
	t := prankFrac(p.elapsed(), prankMessageDur)
	c.FillRect(ui.Rect{0, 0, W, H}, 0x000000)
	prankBlitZoom(c, snap, 1.55)
	prankDim(c, 0.5)
	p.shapeBurst(c, 1+t*0.3)
	prankDim(c, 0.32)

	band := ui.Rect{0, H/2 - 72, W, 146}
	c.FillRect(band, 0x000000)
	c.HLine(0, band.Y, W, 0x00A040)
	c.HLine(0, band.Bottom()-1, W, 0x00A040)

	f := ui.DefaultFont()
	msg := "你已经感染索尔克病毒"
	scale := 4
	for scale > 1 && f.ScaledWidth(msg, scale) > W-80 {
		scale--
	}
	jx, jy := 0, 0
	if !p.gentle {
		h := prankHash(uint32(p.elapsed().Milliseconds() / 70))
		jx = int(h%7) - 3
		jy = int((h>>8)%5) - 2
	}
	col := uint32(0xFF3030)
	if !p.gentle && int(p.elapsed()/(520*time.Millisecond))%2 == 1 {
		col = 0xFFFF00
	}
	f.Scaled(c, (W-f.ScaledWidth(msg, scale))/2+jx, band.Y+4+jy, msg, col, scale)
	sub := "SULKO.EXE · 虚构病毒演示 · 没有修改任何文件"
	f.Text(c, (W-f.Width(sub))/2, band.Y+82, sub, 0x80FFA0)
	note := "（本画面只是动画，演示即将自动结束）"
	f.Text(c, (W-f.Width(note))/2, band.Y+108, note, 0xA0C0FF)
}

func (p *prankDemo) gather(d *Desktop) {
	p.items = p.items[:0]
	cx, cy := float64(d.W)/2, float64(d.taskbarTop())/2
	for i, ic := range d.icons {
		x, y := d.iconPos(i)
		ix, iy := float64(x+40), float64(y+38)
		rad := math.Hypot(ix-cx, iy-cy)
		if rad < 40 {
			rad = 40
		}
		ang := math.Atan2(iy-cy, ix-cx)
		p.items = append(p.items, prankFloat{
			label: ic.label,
			icon:  ic.kind,
			w:     86, h: 64,
			ang: ang, rad: rad,
			spin:  1.6 + float64(i%5)*0.3,
			phase: float64(i) * 0.7,
		})
	}
	for _, w := range d.windows {
		if w.Minimized {
			continue
		}
		ix := float64(w.X + w.W/2)
		iy := float64(w.Y + w.H/2)
		rad := math.Hypot(ix-cx, iy-cy)
		if rad < 60 {
			rad = 60
		}
		ang := math.Atan2(iy-cy, ix-cx)
		p.items = append(p.items, prankFloat{
			label: w.App.Title(),
			win:   true,
			w:     maxInt(150, minInt(260, w.W/3)),
			h:     maxInt(80, minInt(150, w.H/3)),
			ang:   ang, rad: rad,
			spin:  1.2,
			phase: float64(len(p.items)) * 0.5,
		})
	}
	for i := 0; i < 7; i++ {
		ang := float64(i) * 2 * math.Pi / 7
		rad := float64(maxInt(d.W, d.H)) * 0.5
		p.items = append(p.items, prankFloat{
			label: "SULKO.SYS",
			virus: true,
			w:     110, h: 90,
			ang: ang, rad: rad,
			spin:  2.4 + float64(i)*0.25,
			phase: float64(i) * 0.9,
		})
	}
	sort.Slice(p.items, func(i, j int) bool { return p.items[i].rad > p.items[j].rad })
}

func (p *prankDemo) vortexPos(it prankFloat, t float64, W, H int) (int, int) {
	cx, cy := float64(W)/2, float64(H)/2
	ease := t * t
	ang := it.ang + it.spin*(0.6+t*1.4)*math.Pi*2
	rad := it.rad * (1 - 0.96*ease)
	x := cx + math.Cos(ang)*rad - float64(it.w)/2
	y := cy + math.Sin(ang)*rad*0.72 - float64(it.h)/2
	return int(x), int(y)
}

func (p *prankDemo) drawVortex(c *ui.Canvas) {
	W, H := c.W, c.H
	t := prankFrac(p.elapsed(), prankVortexDur)
	c.FillRect(ui.Rect{0, 0, W, H}, 0x050008)
	p.drawVortexBg(c, t)
	for _, it := range p.items {
		x, y := p.vortexPos(it, t, W, H)
		for k := 4; k >= 1; k-- {
			tp := t - float64(k)*0.03
			if tp < 0 {
				tp = 0
			}
			tx, ty := p.vortexPos(it, tp, W, H)
			prankMixRect(c, ui.Rect{tx, ty, it.w, it.h}, 0x20FF80, 16)
		}
		p.drawFloatItem(c, it, x, y)
	}
	p.drawVortexCore(c, t)
}

func (p *prankDemo) drawVortexBg(c *ui.Canvas, t float64) {
	W, H := c.W, c.H
	cx, cy := float64(W)/2, float64(H)/2
	maxR := math.Hypot(float64(W), float64(H))/2 + 20
	speed := 3.0
	if p.gentle {
		speed = 1.0
	}
	for arm := 0; arm < 6; arm++ {
		base := float64(arm) * math.Pi / 3
		for s := 0; s < 240; s++ {
			rf := float64(s) / 240
			r := rf * maxR
			a := base + rf*4.2 + t*speed
			x := int(cx + math.Cos(a)*r)
			y := int(cy + math.Sin(a)*r*0.72)
			col := uint32(0x104020)
			if arm%2 == 0 {
				col = 0x201030
			}
			if s%23 == 0 {
				col = 0x30A060
			}
			prankDisc(c, x, y, 3, col)
		}
	}
	for i := 0; i < 3; i++ {
		prankRing(c, int(cx), int(cy), int(maxR*(0.3+0.18*float64(i))), 2, 0x203050)
	}
}

func (p *prankDemo) drawVortexCore(c *ui.Canvas, t float64) {
	W, H := c.W, c.H
	cx, cy := W/2, H/2
	maxR := math.Hypot(float64(W), float64(H)) / 2
	r := int(maxR * 0.10 * (0.6 + t*1.1))
	prankDisc(c, cx, cy, r+14, 0x1A0030)
	prankRing(c, cx, cy, r+14, 4, 0x40FF80)
	prankRing(c, cx, cy, r+6, 2, 0xFF40C0)
	prankDisc(c, cx, cy, r, 0x000000)
	for i := 0; i < 48; i++ {
		a := float64(i)*0.37 + t*10
		rr := float64(r) * float64(i%9) / 9
		x := cx + int(math.Cos(a)*rr)
		y := cy + int(math.Sin(a)*rr)
		col := uint32(0x80FF80)
		if i%3 == 0 {
			col = 0xFF80E0
		}
		prankDisc(c, x, y, 2, col)
	}
}

func (p *prankDemo) chaosPos(it prankFloat, i int, tt float64, W, H, tick int, jitter bool) (int, int) {
	sp := 1.3 + float64(i%4)*0.35
	if p.gentle {
		sp *= 0.5
	}
	ax := 0.9 + float64(prankHash(uint32(i), 11)%100)/100*1.6
	ay := 0.9 + float64(prankHash(uint32(i), 23)%100)/100*1.6
	x := math.Abs(math.Sin(tt*sp*ax + it.phase))
	y := math.Abs(math.Cos(tt*sp*ay + it.phase*1.3))
	px := int(x * float64(W-it.w))
	py := int(30 + y*float64(H-it.h-60))
	if jitter {
		h := prankHash(uint32(i), uint32(tick))
		px += int(h%13) - 6
		py += int((h>>8)%9) - 4
	}
	if px < 0 {
		px = 0
	}
	if py < 28 {
		py = 28
	}
	if px+it.w > W {
		px = W - it.w
	}
	if py+it.h > H-26 {
		py = H - 26 - it.h
	}
	return px, py
}

func (p *prankDemo) drawChaos(c *ui.Canvas) {
	W, H := c.W, c.H
	c.FillRect(ui.Rect{0, 0, W, H}, 0x050008)
	p.drawVortexBg(c, 1)
	tMove := p.elapsed()
	frozen := tMove >= prankChaosMove
	if frozen {
		tMove = prankChaosMove
	}
	tick := int(tMove / (120 * time.Millisecond))
	if p.gentle {
		tick = int(tMove / (400 * time.Millisecond))
	}
	tt := float64(tMove) / float64(time.Second)
	for i, it := range p.items {
		x, y := p.chaosPos(it, i, tt, W, H, tick, !p.gentle && !frozen)
		if !frozen {
			x2, y2 := p.chaosPos(it, i, tt-0.05, W, H, tick, false)
			prankMixRect(c, ui.Rect{x2, y2, it.w, it.h}, 0xFF4080, 22)
		}
		p.drawFloatItem(c, it, x, y)
	}
	if frozen {
		p.freezeOverlay(c)
	}
}

func (p *prankDemo) freezeOverlay(c *ui.Canvas) {
	W, H := c.W, c.H
	prankDim(c, 0.35)
	for i := 0; i < 5; i++ {
		h := prankHash(uint32(i + 400))
		y := int(h % uint32(H))
		c.FillRect(ui.Rect{int(h%40) - 40, y, W, 2 + int((h>>8)%5)}, 0x101020)
	}
	w := 430
	if w > W-40 {
		w = W - 40
	}
	h := 152
	r := ui.Rect{(W - w) / 2, (H - h) / 2, w, h}
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	f := ui.DefaultFont()
	f.TextBold(c, r.X+14, r.Y+12, "SULKO.EXE 没有响应", ui.ColText)
	f.Text(c, r.X+14, r.Y+38, "程序已停止响应，系统即将崩溃（虚构演示）。", ui.ColText)
	f.Text(c, r.X+14, r.Y+60, "没有文件受到影响，演示即将自动结束。", 0x006000)
	ui.Button(c, ui.Rect{r.X + r.W - 160, r.Y + h - 38, 70, 24}, "关闭", false, true)
	ui.Button(c, ui.Rect{r.X + r.W - 84, r.Y + h - 38, 70, 24}, "等待", false, true)
}

func (p *prankDemo) drawBSOD(c *ui.Canvas) {
	W, H := c.W, c.H
	f := ui.DefaultFont()
	c.FillRect(ui.Rect{0, 0, W, H}, 0x0000AA)

	y := 48
	f.TextBold(c, 32, y, "Windows", 0xFFFFFF)
	y += 30
	f.TextBold(c, 32, y, "A fatal exception 0x0000SULKO has occurred at 0028:C0011E36 in VXD SULKO(01) +", 0xFFFFFF)
	y += 22
	f.Text(c, 32, y, "00010E36. The current application will be terminated.", 0xFFFFFF)
	y += 34
	lines := []string{
		"*  没有格式化任何磁盘，也没有修改分区表。",
		"*  没有删除或移动任何文件，所有窗口仍在原处。",
		"*  刚才的六阶段病毒画面只是内存中的模拟动画。",
		"*  演示无法中途退出，即将自动结束，请稍候。",
	}
	for _, line := range lines {
		f.Text(c, 32, y, line, 0xFFFFA0)
		y += 22
	}
	y += 14
	f.Text(c, 32, y, "Please wait for automatic recovery. The demo will end by itself.", 0xFFFFFF)
	y += 30
	dots := ""
	n := int(p.elapsed().Milliseconds()/400) % 4
	for i := 0; i < n; i++ {
		dots += "."
	}
	f.Text(c, 32, y, "正在恢复桌面"+dots, 0xC0D0FF)

	bottom := "演示结束后，桌面、窗口和文件都会保持原样。"
	f.Text(c, (W-f.Width(bottom))/2, H-52, bottom, 0x9FB0E0)
}

// updatePopups spawns and expires the fake DEAD terminals and virus dialogs
// that litter the overlay during the chaotic stages.
func (p *prankDemo) updatePopups(d *Desktop, now time.Time) {
	alive := p.popups[:0]
	for _, pp := range p.popups {
		if now.Sub(pp.born) < pp.life {
			alive = append(alive, pp)
		}
	}
	p.popups = alive
	if p.stage < prankStageDance || p.stage > prankStageChaos {
		return
	}
	if p.nextPopupAt.IsZero() {
		p.nextPopupAt = now.Add(500 * time.Millisecond)
	}
	if now.Before(p.nextPopupAt) {
		return
	}
	maxAlive, interval := 6, 620*time.Millisecond
	if p.gentle {
		maxAlive, interval = 3, 1400*time.Millisecond
	}
	if len(p.popups) < maxAlive {
		p.spawnPopup(d, now)
	}
	jitter := time.Duration(prankHash(p.popupSeq, 99)%400) * time.Millisecond
	p.nextPopupAt = now.Add(interval + jitter)
}

func (p *prankDemo) spawnPopup(d *Desktop, now time.Time) {
	p.popupSeq++
	h := prankHash(p.popupSeq, 777)
	terminal := h%2 == 0
	w, hh := 280, 170
	if !terminal {
		w, hh = 320, 140
	}
	if w > d.W-20 {
		w = d.W - 20
	}
	if hh > d.H-100 {
		hh = d.H - 100
	}
	x := int(h>>8) % maxInt(d.W-w, 1)
	y := 30 + int(h>>16)%maxInt(d.H-hh-70, 1)
	life := 1100*time.Millisecond + time.Duration(h%500)*time.Millisecond
	if p.gentle {
		life = 1800 * time.Millisecond
	}
	p.popups = append(p.popups, prankPopup{x: x, y: y, w: w, h: hh, born: now, life: life, terminal: terminal, seed: h})
}

func (p *prankDemo) drawPopups(c *ui.Canvas, now time.Time) {
	for _, pp := range p.popups {
		if pp.terminal {
			p.drawDeadTerminal(c, pp, now)
		} else {
			p.drawVirusDialog(c, pp)
		}
	}
}

func (p *prankDemo) drawDeadTerminal(c *ui.Canvas, pp prankPopup, now time.Time) {
	r := ui.Rect{pp.x, pp.y, pp.w, pp.h}
	c.FillRect(r, 0x000000)
	c.Raised(r)
	tb := ui.Rect{r.X + 2, r.Y + 2, r.W - 4, 18}
	c.FillRect(tb, 0x000080)
	f := ui.DefaultFont()
	f.TextClip(c, tb.X+4, tb.Y+2, tb.W-8, "命令提示符 - SULKO.EXE", 0xFFFFFF)
	f.Text(c, r.X+10, r.Y+28, "C:\\WINDOWS> SULKO.EXE", 0x00FF60)
	col := uint32(0xFF3030)
	if !p.gentle && int(now.Sub(pp.born)/(300*time.Millisecond))%2 == 1 {
		col = 0xFFFF00
	}
	f.Scaled(c, r.X+10, r.Y+50, "DEAD", col, 3)
	f.Text(c, r.X+10, r.Y+104, "DEAD  DEAD  DEAD  DEAD", 0x00C040)
	f.Text(c, r.X+10, r.Y+126, "系统已死亡（模拟，未受影响）", 0x808080)
}

func (p *prankDemo) drawVirusDialog(c *ui.Canvas, pp prankPopup) {
	r := ui.Rect{pp.x, pp.y, pp.w, pp.h}
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	tb := ui.Rect{r.X + 2, r.Y + 2, r.W - 4, 18}
	c.FillRect(tb, 0x000080)
	f := ui.DefaultFont()
	titles := []string{"系统错误", "SULKO 警告", "致命错误", "Windows"}
	title := titles[int(pp.seed>>4)%len(titles)]
	f.TextClip(c, tb.X+4, tb.Y+2, tb.W-8, title, 0xFFFFFF)
	cx, cy := r.X+28, r.Y+52
	prankDisc(c, cx, cy, 14, 0xC00000)
	for i := -6; i <= 6; i++ {
		c.Set(cx+i, cy+i, 0xFFFFFF)
		c.Set(cx+i, cy-i, 0xFFFFFF)
		c.Set(cx+i+1, cy+i, 0xFFFFFF)
		c.Set(cx+i+1, cy-i, 0xFFFFFF)
	}
	msgs := []string{"你中了苏尔科病毒！", "苏尔科病毒已激活！", "你的电脑已被苏尔科控制！"}
	msg := msgs[int(pp.seed>>12)%len(msgs)]
	f.TextBold(c, r.X+52, r.Y+38, msg, 0xA00000)
	f.Text(c, r.X+52, r.Y+62, "这只是视觉演示，没有文件受影响。", ui.ColText)
	btn := ui.Rect{r.X + r.W/2 - 40, r.Y + r.H - 34, 80, 24}
	ui.Button(c, btn, "确定", false, true)
}

func prankInvert(c *ui.Canvas) {
	for i, v := range c.Pix {
		c.Pix[i] = v ^ 0xFFFFFF
	}
}

func prankBlit(c, src *ui.Canvas, dx, dy int) {
	x0, x1 := 0, src.W
	if dx < 0 {
		x0 = -dx
	}
	if dx+x1 > c.W {
		x1 = c.W - dx
	}
	if x0 >= x1 {
		return
	}
	y0, y1 := 0, src.H
	if dy < 0 {
		y0 = -dy
	}
	if dy+y1 > c.H {
		y1 = c.H - dy
	}
	if y0 >= y1 {
		return
	}
	for y := y0; y < y1; y++ {
		copy(c.Pix[(dy+y)*c.Stride+dx+x0:(dy+y)*c.Stride+dx+x1], src.Pix[y*src.Stride+x0:y*src.Stride+x1])
	}
}

func prankBlitWrap(c, src *ui.Canvas, dx, dy int) {
	W, H := src.W, src.H
	for oy := -1; oy <= 1; oy++ {
		ty := dy + oy*H
		if ty <= -H || ty >= c.H {
			continue
		}
		for ox := -1; ox <= 1; ox++ {
			tx := dx + ox*W
			if tx <= -W || tx >= c.W {
				continue
			}
			prankBlit(c, src, tx, ty)
		}
	}
}

// prankBlitZoom magnifies the snapshot while keeping its bottom-left corner
// pinned, matching the "shapes explode from the lower-left" stage.
func prankBlitZoom(c, src *ui.Canvas, zoom float64) {
	if zoom < 1 {
		zoom = 1
	}
	if zoom > 4 {
		zoom = 4
	}
	for dy := 0; dy < c.H; dy++ {
		sy := int(float64(src.H-1) - float64(c.H-1-dy)/zoom)
		if sy < 0 || sy >= src.H {
			continue
		}
		dst := c.Pix[dy*c.Stride:]
		srow := src.Pix[sy*src.Stride:]
		for dx := 0; dx < c.W; dx++ {
			sx := int(float64(dx) / zoom)
			if sx >= src.W {
				break
			}
			dst[dx] = srow[sx]
		}
	}
}

func prankMix(dst, src uint32, a int) uint32 {
	if a <= 0 {
		return dst
	}
	if a >= 256 {
		return src
	}
	ia := uint32(256 - a)
	ua := uint32(a)
	r := (((dst>>16)&0xFF)*ia + ((src>>16)&0xFF)*ua) >> 8
	g := (((dst>>8)&0xFF)*ia + ((src>>8)&0xFF)*ua) >> 8
	b := ((dst&0xFF)*ia + (src&0xFF)*ua) >> 8
	return r<<16 | g<<8 | b
}

func prankDim(c *ui.Canvas, amount float64) {
	a := int(amount * 256)
	if a <= 0 {
		return
	}
	if a > 255 {
		a = 255
	}
	for i, v := range c.Pix {
		c.Pix[i] = prankMix(v, 0x000000, a)
	}
}

func prankMixRect(c *ui.Canvas, r ui.Rect, col uint32, a int) {
	if r.W <= 0 || r.H <= 0 || a <= 0 {
		return
	}
	x0, y0 := r.X, r.Y
	x1, y1 := r.Right(), r.Bottom()
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.W {
		x1 = c.W
	}
	if y1 > c.H {
		y1 = c.H
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := y*c.Stride + x
			c.Pix[i] = prankMix(c.Pix[i], col, a)
		}
	}
}

func prankMixRow(c *ui.Canvas, y int, col uint32, a int) {
	if y < 0 || y >= c.H || a <= 0 {
		return
	}
	for x := 0; x < c.W; x++ {
		i := y*c.Stride + x
		c.Pix[i] = prankMix(c.Pix[i], col, a)
	}
}

func prankDisc(c *ui.Canvas, cx, cy, r int, col uint32) {
	if r < 1 {
		r = 1
	}
	for y := -r; y <= r; y++ {
		span := int(math.Sqrt(float64(r*r - y*y)))
		for x := -span; x <= span; x++ {
			c.Set(cx+x, cy+y, col)
		}
	}
}

func prankRing(c *ui.Canvas, cx, cy, r, th int, col uint32) {
	if r < 1 {
		return
	}
	if th < 1 {
		th = 1
	}
	steps := int(2*math.Pi*float64(r)) + 8
	if steps > 4000 {
		steps = 4000
	}
	for i := 0; i < steps; i++ {
		a := 2 * math.Pi * float64(i) / float64(steps)
		for k := 0; k < th; k++ {
			c.Set(cx+int(math.Cos(a)*float64(r-k)), cy+int(math.Sin(a)*float64(r-k)), col)
		}
	}
}

type prankPt struct{ x, y float64 }

func prankPoly(c *ui.Canvas, cx, cy, r, sides int, rot float64, col uint32) {
	if sides < 3 {
		sides = 3
	}
	pts := make([]prankPt, sides)
	for i := range pts {
		a := rot + 2*math.Pi*float64(i)/float64(sides)
		pts[i] = prankPt{
			x: float64(cx) + math.Cos(a)*float64(r),
			y: float64(cy) + math.Sin(a)*float64(r),
		}
	}
	prankFillPoly(c, pts, col)
}

func prankFillPoly(c *ui.Canvas, pts []prankPt, col uint32) {
	if len(pts) < 3 {
		return
	}
	minY, maxY := pts[0].y, pts[0].y
	for _, p := range pts[1:] {
		if p.y < minY {
			minY = p.y
		}
		if p.y > maxY {
			maxY = p.y
		}
	}
	y0 := int(math.Floor(minY))
	y1 := int(math.Ceil(maxY))
	if y0 < 0 {
		y0 = 0
	}
	if y1 >= c.H {
		y1 = c.H - 1
	}
	xs := make([]float64, 0, 8)
	for y := y0; y <= y1; y++ {
		xs = xs[:0]
		fy := float64(y) + 0.5
		for i := 0; i < len(pts); i++ {
			a := pts[i]
			b := pts[(i+1)%len(pts)]
			if (a.y <= fy && b.y > fy) || (b.y <= fy && a.y > fy) {
				tt := (fy - a.y) / (b.y - a.y)
				xs = append(xs, a.x+tt*(b.x-a.x))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Round(xs[i]))
			x1 := int(math.Round(xs[i+1])) - 1
			for x := x0; x <= x1; x++ {
				c.Set(x, y, col)
			}
		}
	}
}

func (p *prankDemo) shapeBurst(c *ui.Canvas, t float64) {
	W, H := c.W, c.H
	tick := int(p.elapsed() / (160 * time.Millisecond))
	if p.gentle {
		tick = int(p.elapsed() / (700 * time.Millisecond))
	}
	colors := []uint32{0xFF0050, 0xFFD000, 0x00FF80, 0x00C0FF, 0xFF00FF, 0xFFFFFF, 0xFF6000}
	n := 26
	if p.gentle {
		n = 12
	}
	for i := 0; i < n; i++ {
		h := prankHash(uint32(i+31), uint32(tick/2))
		phase := float64(prankHash(uint32(i+5))%1000) / 1000.0
		rad := math.Mod(phase+t*(0.9+float64(i%3)*0.25), 1.0) * float64(W) * 1.15
		ang := float64(h%100) / 100.0 * math.Pi * 0.5
		cx := 10 + int(math.Cos(ang)*rad)
		cy := H - 20 - int(math.Sin(ang)*rad)
		size := 16 + int((h>>9)%46) + int(t*50)
		col := colors[int((h>>16)%uint32(len(colors)))]
		rot := t*3 + phase*6
		switch i % 4 {
		case 0:
			prankPoly(c, cx, cy, size, 4, rot, col)
		case 1:
			prankPoly(c, cx, cy, size, 3, rot, col)
		case 2:
			prankDisc(c, cx, cy, size/2, col)
		case 3:
			prankRing(c, cx, cy, size, 2+int(t*4), col)
		}
	}
	m := 10
	if p.gentle {
		m = 5
	}
	for i := 0; i < m; i++ {
		h := prankHash(uint32(i+201), uint32(tick/6))
		cx := 50 + int(h%uint32(maxInt(W-100, 1)))
		cy := 60 + int((h>>10)%uint32(maxInt(H-140, 1)))
		size := 20 + int((h>>16)%52)
		rot := float64(tick)*0.18 + float64(h%9)
		col := colors[i%len(colors)]
		switch i % 3 {
		case 0:
			prankPoly(c, cx, cy, size, 4, rot, col)
		case 1:
			prankPoly(c, cx, cy, size, 3, rot, col)
		default:
			prankRing(c, cx, cy, size, 2, col)
		}
	}
}

func (p *prankDemo) drawFloatItem(c *ui.Canvas, it prankFloat, x, y int) {
	f := ui.DefaultFont()
	if it.win {
		r := ui.Rect{x, y, it.w, it.h}
		c.FillRect(r, 0xC0C0C0)
		c.Raised(r)
		tb := ui.Rect{x + 2, y + 2, it.w - 4, 16}
		c.FillRect(tb, 0x000080)
		f.TextClip(c, tb.X+4, tb.Y+1, tb.W-6, it.label, 0xFFFFFF)
		for i := 0; i < (it.h-24)/7; i++ {
			col := uint32(0x808080)
			if i%3 == 0 {
				col = 0x000080
			}
			c.HLine(x+6, y+22+i*7, it.w-12, col)
		}
		return
	}
	if it.virus {
		prankVirusIcon(c, x+it.w/2, y+it.h/2-6, it.h-20)
		f.TextClip(c, x, y+it.h-16, it.w, "SULKO.SYS", 0xD0FFD0)
		return
	}
	ui.DrawIcon32(c, it.icon, x+it.w/2-16, y+4, false)
	tw := f.Width(it.label)
	if tw > it.w {
		tw = it.w
	}
	f.TextClip(c, x+(it.w-tw)/2, y+42, tw, it.label, 0xFFFFFF)
}

func prankVirusIcon(c *ui.Canvas, cx, cy, size int) {
	r := size / 2
	if r < 8 {
		r = 8
	}
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		x := cx + int(math.Cos(a)*float64(r-2))
		y := cy + int(math.Sin(a)*float64(r-2))
		prankDisc(c, x, y, r/6+1, 0x70FF70)
	}
	prankDisc(c, cx, cy, r-2, 0x107010)
	prankDisc(c, cx, cy, r-4, 0x30C030)
	prankDisc(c, cx-r/3, cy-r/5, r/5, 0x000000)
	prankDisc(c, cx+r/3, cy-r/5, r/5, 0x000000)
	prankDisc(c, cx-r/3, cy-r/5, r/10, 0xFF3030)
	prankDisc(c, cx+r/3, cy-r/5, r/10, 0xFF3030)
	for x := -r / 2; x <= r/2; x++ {
		my := cy + r/3 + int(math.Sin(float64(x)/float64(r)*math.Pi)*4)
		c.Set(cx+x, my, 0x000000)
	}
}

func prankHash(seeds ...uint32) uint32 {
	h := uint32(0x7F4A7C15)
	for _, x := range seeds {
		h = h*0x9E3779B1 + x + 0x7F4A7C15
		h ^= h >> 15
		h *= 0x85EBCA77
		h ^= h >> 13
	}
	return h
}
