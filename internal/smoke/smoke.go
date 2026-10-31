package smoke

import (
	"fmt"
	"strings"

	"win95linux/internal/ui"
)

type Info struct {
	Display  string
	FBDev    string
	Keyboard string
	Mouse    string
	Disk     string
	Memory   string
	Network  string
	Firmware string
	Kernel   string
}

type Modal struct {
	Title  string
	Lines  []string
	Action string
}

type Screen struct {
	Info       Info
	WindowX    int
	WindowY    int
	MouseX     int
	MouseY     int
	Clock      string
	LastKey    string
	ScanCount  int
	OnRescan   func() Info
	OnShutdown func()

	menuOpen  bool
	press     string
	hover     string
	dragging  bool
	dragOffX  int
	dragOffY  int
	mouseDown bool
	modal     *Modal
}

const (
	winW = 820
	winH = 580
)

func New(info Info) *Screen {
	s := &Screen{
		Info:      info,
		MouseX:    512,
		MouseY:    384,
		Clock:     "00:00",
		LastKey:   "—",
		ScanCount: 1,
	}
	return s
}

type layout struct {
	outer    ui.Rect
	title    ui.Rect
	btnMin   ui.Rect
	btnMax   ui.Rect
	btnClose ui.Rect
	client   ui.Rect
	sidebar  ui.Rect
	content  ui.Rect
	panel    ui.Rect
	rescan   ui.Rect
	next     ui.Rect
	start    ui.Rect
	task     ui.Rect
	clock    ui.Rect
	menu     ui.Rect
	modal    ui.Rect
	modalOK  ui.Rect
	modalX   ui.Rect
}

func (s *Screen) layout() layout {
	var l layout
	l.outer = ui.Rect{s.WindowX, s.WindowY, winW, winH}
	l.title = ui.Rect{l.outer.X + 3, l.outer.Y + 3, winW - 6, 26}
	l.btnClose = ui.Rect{l.title.Right() - 23, l.title.Y + 4, 20, 18}
	l.btnMax = ui.Rect{l.title.Right() - 45, l.title.Y + 4, 20, 18}
	l.btnMin = ui.Rect{l.title.Right() - 67, l.title.Y + 4, 20, 18}
	l.client = ui.Rect{l.outer.X + 3, l.title.Bottom(), winW - 6, winH - 32}
	l.sidebar = ui.Rect{l.client.X, l.client.Y, 210, l.client.H}
	l.content = ui.Rect{l.sidebar.Right(), l.client.Y, l.client.W - 210, l.client.H}
	l.panel = ui.Rect{l.content.X + 28, l.client.Y + 100, l.content.W - 56, 348}
	l.next = ui.Rect{l.client.Right() - 28 - 140, l.client.Y + 478, 140, 28}
	l.rescan = ui.Rect{l.next.X - 8 - 128, l.next.Y, 128, 28}
	l.start = ui.Rect{4, s.taskbarTop() + 4, 86, 26}
	l.task = ui.Rect{96, s.taskbarTop() + 4, 150, 26}
	l.clock = ui.Rect{s.screenW() - 92, s.taskbarTop() + 5, 84, 24}
	l.menu = ui.Rect{4, s.taskbarTop() - 194, 236, 192}
	l.modal = ui.Rect{(s.screenW() - 460) / 2, (s.taskbarTop() - 196) / 2, 460, 196}
	l.modalOK = ui.Rect{l.modal.X + (l.modal.W-96)/2, l.modal.Bottom() - 44, 96, 28}
	l.modalX = ui.Rect{l.modal.X + l.modal.W - 25, l.modal.Y + 5, 20, 18}
	return l
}

var screenW, screenH = 1024, 768

func (s *Screen) SetScreenSize(w, h int) {
	screenW, screenH = w, h
}

func (s *Screen) screenW() int { return screenW }
func (s *Screen) screenH() int { return screenH }

func (s *Screen) taskbarTop() int { return screenH - 34 }

func (s *Screen) Center() {
	s.WindowX = (screenW - winW) / 2
	s.WindowY = (screenH - 34 - winH) / 2
}

func (s *Screen) Draw(c *ui.Canvas) {
	c.DesktopPattern(ui.Rect{0, 0, c.W, c.H})
	s.drawDesktopIcons(c)
	s.drawTaskbar(c)
	s.drawWindow(c)
	if s.menuOpen {
		s.drawStartMenu(c)
	}
	if s.modal != nil {
		s.drawModal(c)
	}
	ui.Cursor(c, s.MouseX, s.MouseY)
}

func (s *Screen) drawDesktopIcons(c *ui.Canvas) {
	f := ui.DefaultFont()
	ui.IconComputer(c, 28, 28, 2, false)
	s.centerLabel(c, f, "我的电脑", 28+32, 98)
	ui.IconRecycle(c, 28, 140, 2)
	s.centerLabel(c, f, "回收站", 28+32, 210)
}

func (s *Screen) centerLabel(c *ui.Canvas, f *ui.Font, text string, cx, y int) {
	x := cx - f.Width(text)/2
	f.Text(c, x+1, y+1, text, ui.ColDark)
	f.Text(c, x, y, text, ui.ColWhite)
}

func drawFit(c *ui.Canvas, f *ui.Font, x, y, maxW int, s string, col uint32) {
	if f.Width(s) <= maxW {
		f.Text(c, x, y, s, col)
		return
	}
	const ell = "…"
	ew := f.Width(ell)
	w := 0
	var b strings.Builder
	for _, r := range s {
		rw := f.Width(string(r))
		if w+rw+ew > maxW {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteString(ell)
	f.Text(c, x, y, b.String(), col)
}

func (s *Screen) drawTaskbar(c *ui.Canvas) {
	l := s.layout()
	top := s.taskbarTop()
	bar := ui.Rect{0, top, c.W, 34}
	c.FillRect(bar, ui.ColFace)
	c.HLine(0, top, c.W, ui.ColWhite)
	c.HLine(0, top+1, c.W, ui.ColLight)
	c.HLine(0, top+2, c.W, ui.ColShadow)

	ui.Button(c, l.start, "开始", s.menuOpen, true)
	ui.FlagLogo(c, l.start.X+7, l.start.Y+5, 16)
	f := ui.DefaultFont()
	f.TextBold(c, l.start.X+30, l.start.Y+5, "开始", ui.ColText)

	ui.Button(c, l.task, "系统自检", true, true)

	clock := l.clock
	c.FillRect(clock, ui.ColFace)
	c.Sunken(clock)
	f.Text(c, clock.X+(clock.W-f.Width(s.Clock))/2, clock.Y+4, s.Clock, ui.ColText)
	c.HLine(clock.X+3, clock.Bottom()-3, clock.W-6, ui.ColLight)
}

func (s *Screen) drawWindow(c *ui.Canvas) {
	l := s.layout()
	c.FillRect(l.outer, ui.ColFace)
	c.Raised(l.outer)
	c.TitleBar(l.title, ui.ColNavy, ui.ColNavyLight)
	f := ui.DefaultFont()
	ui.FlagLogo(c, l.title.X+5, l.title.Y+5, 16)
	f.TextBold(c, l.title.X+26, l.title.Y+6, "Windows 95 Linux 安装程序", ui.ColWhite)
	ui.SmallButton(c, l.btnMin, "_", s.press == "min")
	ui.SmallButton(c, l.btnMax, "□", s.press == "max")
	ui.SmallButton(c, l.btnClose, "×", s.press == "close")

	c.FillRect(l.client, ui.ColFace)
	s.drawSidebar(c, l)
	s.drawContent(c, l)
}

func (s *Screen) drawSidebar(c *ui.Canvas, l layout) {
	f := ui.DefaultFont()
	c.TitleBar(l.sidebar, 0x000060, 0x000080)
	for x := l.sidebar.X; x < l.sidebar.Right(); x += 2 {
		c.Set(x, l.sidebar.Y, 0x0000A0)
	}
	ui.FlagLogo(c, l.sidebar.X+26, l.client.Y+26, 44)
	x := l.sidebar.X + 24
	f.Scaled(c, x, l.client.Y+86, "Windows", ui.ColWhite, 2)
	f.Scaled(c, x+f.Width("Windows")*2+6, l.client.Y+86, "95", ui.ColYellow, 2)
	f.TextBold(c, x, l.client.Y+126, "Linux 中文版", ui.ColWhite)
	c.FillRect(ui.Rect{x, l.client.Y + 150, 162, 1}, 0x4080C0)
	f.Scaled(c, x, l.client.Y+170, "安装程序", ui.ColWhite, 2)
	f.Text(c, x, l.client.Y+212, "系统自检", 0xC0E0FF)
	f.Text(c, x, l.client.Y+232, "安装向导 第 1 步", 0xC0E0FF)

	bottom := l.client.Bottom()
	f.Text(c, x, bottom-72, "版本 26.1", 0xC0E0FF)
	f.Text(c, x, bottom-50, "ARM64 · UEFI", 0xC0E0FF)
	f.Text(c, x, bottom-28, "© 2026 Win95 Linux", 0x80C0FF)
}

func (s *Screen) drawContent(c *ui.Canvas, l layout) {
	f := ui.DefaultFont()
	c.FillRect(l.content, ui.ColFace)
	c.VLine(l.content.X, l.content.Y, l.content.H, ui.ColDark)
	c.VLine(l.content.X+1, l.content.Y, l.content.H, ui.ColLight)

	x := l.content.X + 28
	f.Scaled(c, x, l.client.Y+22, "系统自检", ui.ColText, 2)
	f.Text(c, x, l.client.Y+64, "安装程序正在检查您的计算机，请稍候…", ui.ColText)
	c.FillRect(ui.Rect{x, l.client.Y + 88, l.content.W - 56, 1}, ui.ColShadow)
	c.FillRect(ui.Rect{x, l.client.Y + 89, l.content.W - 56, 1}, ui.ColWhite)

	c.FillRect(l.panel, ui.ColField)
	c.Sunken(l.panel)

	rows := []struct {
		label  string
		value  string
		value2 string
		h      int
	}{
		{"显示适配器", s.Info.Display, s.Info.FBDev, 48},
		{"输入设备", s.Info.Keyboard, s.Info.Mouse, 48},
		{"硬盘", s.Info.Disk, "", 40},
		{"内存", s.Info.Memory, "", 40},
		{"网络", s.Info.Network, "", 40},
		{"固件", s.Info.Firmware, "", 40},
		{"操作系统", s.Info.Kernel, "", 40},
		{"按键测试", fmt.Sprintf("%s（请按任意键）", s.LastKey), "", 40},
	}
	y := l.panel.Y + 6
	maxW := l.panel.Right() - (l.panel.X + 158) - 12
	for i, row := range rows {
		if i > 0 {
			c.HLine(l.panel.X+10, y-3, l.panel.W-20, ui.ColLight)
		}
		ui.CheckMark(c, l.panel.X+16, y+(row.h-16)/2, ui.ColGreen)
		f.TextBold(c, l.panel.X+42, y+5, row.label, ui.ColText)
		drawFit(c, f, l.panel.X+158, y+5, maxW, row.value, ui.ColText)
		if row.value2 != "" {
			drawFit(c, f, l.panel.X+158, y+23, maxW, row.value2, ui.ColShadow)
		}
		y += row.h
	}

	ui.Button(c, l.rescan, "重新检测(R)", s.press == "rescan", true)
	ui.Button(c, l.next, "下一步(N) >", s.press == "next", true)

	ui.CheckMark(c, l.panel.X+2, l.next.Y+4, ui.ColGreen)
	f.Text(c, l.panel.X+22, l.next.Y+4, fmt.Sprintf("自检项目已通过（第 %d 次）", s.ScanCount), ui.ColText)
	f.Text(c, l.panel.X+2, l.next.Y+30, "提示：可拖动窗口，按 F5 重新检测。", ui.ColShadow)
}

func (s *Screen) drawStartMenu(c *ui.Canvas) {
	l := s.layout()
	r := l.menu
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	f := ui.DefaultFont()
	c.TitleBar(ui.Rect{r.X + 3, r.Y + 3, 24, r.H - 6}, 0x000060, 0x000080)
	for i, ch := range "Windows95" {
		f.Text(c, r.X+7, r.Y+8+i*16, string(ch), 0xFFFFFF)
	}
	items := []struct {
		id    string
		label string
		on    bool
		sep   bool
	}{
		{"prog", "程序(P)", false, false},
		{"docs", "文档(D)", false, false},
		{"settings", "设置(S)", false, false},
		{"", "", false, true},
		{"about", "关于自检盘(A)…", true, false},
		{"shutdown", "关闭系统(U)…", true, false},
	}
	y := r.Y + 6
	x := r.X + 30
	for _, it := range items {
		if it.sep {
			c.HLine(x, y+3, r.W-36, ui.ColShadow)
			c.HLine(x, y+4, r.W-36, ui.ColWhite)
			y += 9
			continue
		}
		ir := ui.Rect{x - 2, y, r.W - 32, 26}
		if s.hover == it.id && it.on {
			c.FillRect(ir, ui.ColNavy)
			f.Text(c, x+4, y+5, it.label, ui.ColWhite)
		} else {
			col := ui.ColText
			if !it.on {
				col = ui.ColTextDis
			}
			f.Text(c, x+4, y+5, it.label, col)
		}
		if it.id == "prog" || it.id == "docs" || it.id == "settings" {
			f.Text(c, r.Right()-24, y+5, "▶", ui.ColTextDis)
		}
		y += 28
	}
}

func (s *Screen) drawModal(c *ui.Canvas) {
	m := s.modal
	l := s.layout()
	r := l.modal
	c.DesktopPattern(ui.Rect{0, 0, c.W, c.H})
	s.drawDesktopIcons(c)
	s.drawTaskbar(c)
	s.drawWindow(c)
	if s.menuOpen {
		s.drawStartMenu(c)
	}
	c.FillRect(r, ui.ColFace)
	c.Raised(r)
	title := ui.Rect{r.X + 3, r.Y + 3, r.W - 6, 26}
	c.TitleBar(title, ui.ColNavy, ui.ColNavyLight)
	f := ui.DefaultFont()
	f.TextBold(c, title.X+8, title.Y+6, m.Title, ui.ColWhite)
	ui.SmallButton(c, l.modalX, "×", s.press == "modalclose")

	icon := ui.Rect{r.X + 26, r.Y + 52, 34, 34}
	c.FillRect(icon, ui.ColNavy)
	c.Raised(icon)
	f.Scaled(c, icon.X+11, icon.Y+2, "i", ui.ColWhite, 2)
	ty := r.Y + 48
	for _, line := range m.Lines {
		f.Text(c, r.X+76, ty, line, ui.ColText)
		ty += 24
	}
	ui.Button(c, l.modalOK, "确定", s.press == "modalok", true)
}

func (s *Screen) SetInfo(info Info) {
	s.Info = info
}

func (s *Screen) ShowNext() {
	s.modal = &Modal{
		Title: "自检通过",
		Lines: []string{
			"系统自检已通过，硬件工作正常。",
			"当前为自检镜像，下一步将进入正式安装程序。",
		},
		Action: "next",
	}
}

func (s *Screen) showAbout() {
	s.modal = &Modal{
		Title: "关于自检盘",
		Lines: []string{
			"Windows 95 Linux 自检盘　版本 26.1",
			"ARM64 · UEFI · VirtIO · 中文界面",
		},
		Action: "about",
	}
}

func (s *Screen) Key(name string) {
	s.LastKey = name
	if name == "F5" {
		s.rescan()
		return
	}
	if s.modal != nil {
		if name == "Enter" || name == "Esc" {
			s.modal = nil
		}
		return
	}
	switch name {
	case "Enter":
		s.ShowNext()
	case "Esc":
		s.modal = nil
	}
}

func (s *Screen) rescan() {
	if s.OnRescan != nil {
		s.Info = s.OnRescan()
		s.ScanCount++
	}
}

func (s *Screen) MouseMove(x, y int) {
	s.MouseX = x
	s.MouseY = y
	if s.dragging {
		nx := x - s.dragOffX
		ny := y - s.dragOffY
		if nx < -winW+160 {
			nx = -winW + 160
		}
		if nx > screenW-160 {
			nx = screenW - 160
		}
		if ny < 0 {
			ny = 0
		}
		if ny > screenH-60 {
			ny = screenH - 60
		}
		s.WindowX, s.WindowY = nx, ny
		return
	}
	if s.menuOpen {
		s.hover = s.menuItemAt(x, y)
	}
}

func (s *Screen) menuItemAt(x, y int) string {
	l := s.layout()
	r := l.menu
	x0 := r.X + 28
	items := []string{"prog", "docs", "settings", "", "about", "shutdown"}
	yy := r.Y + 6
	for _, id := range items {
		if id == "" {
			yy += 9
			continue
		}
		ir := ui.Rect{x0 - 2, yy, r.W - 32, 26}
		if ir.Contains(x, y) {
			return id
		}
		yy += 28
	}
	return ""
}

func (s *Screen) MouseDown(x, y int) {
	s.MouseMove(x, y)
	s.mouseDown = true
	l := s.layout()

	if s.modal != nil {
		switch {
		case l.modalOK.Contains(x, y):
			s.press = "modalok"
		case l.modalX.Contains(x, y):
			s.press = "modalclose"
		}
		return
	}

	if s.menuOpen {
		id := s.menuItemAt(x, y)
		if id == "about" {
			s.menuOpen = false
			s.showAbout()
		} else if id == "shutdown" {
			s.menuOpen = false
			s.modal = &Modal{Title: "关闭系统", Lines: []string{"自检盘不能关机，请直接关闭虚拟机窗口。"}, Action: "shutdown"}
		} else {
			s.menuOpen = false
		}
		return
	}

	switch {
	case l.btnClose.Contains(x, y):
		s.press = "close"
	case l.btnMax.Contains(x, y):
		s.press = "max"
	case l.btnMin.Contains(x, y):
		s.press = "min"
	case l.start.Contains(x, y):
		s.press = "start"
	case l.task.Contains(x, y):
		s.press = "task"
	case l.rescan.Contains(x, y):
		s.press = "rescan"
	case l.next.Contains(x, y):
		s.press = "next"
	case l.title.Contains(x, y):
		s.dragging = true
		s.dragOffX = x - s.WindowX
		s.dragOffY = y - s.WindowY
	}
}

func (s *Screen) MouseUp(x, y int) {
	s.MouseMove(x, y)
	s.mouseDown = false
	if s.dragging {
		s.dragging = false
		s.press = ""
		return
	}
	l := s.layout()
	pressed := s.press
	s.press = ""
	switch pressed {
	case "modalok":
		if l.modalOK.Contains(x, y) {
			s.modal = nil
		}
	case "modalclose":
		if l.modalX.Contains(x, y) {
			s.modal = nil
		}
	case "close":
		if l.btnClose.Contains(x, y) {
			s.modal = &Modal{Title: "提示", Lines: []string{"自检窗口不能关闭。", "请继续下一步或直接关闭虚拟机。"}, Action: "close"}
		}
	case "max":
		if l.btnMax.Contains(x, y) {
			s.modal = &Modal{Title: "提示", Lines: []string{"自检模式不支持最大化窗口。"}, Action: "max"}
		}
	case "min":
		if l.btnMin.Contains(x, y) {
			s.modal = &Modal{Title: "提示", Lines: []string{"自检模式不支持最小化窗口。"}, Action: "min"}
		}
	case "start":
		if l.start.Contains(x, y) {
			s.menuOpen = true
			s.hover = ""
		}
	case "rescan":
		if l.rescan.Contains(x, y) {
			s.rescan()
		}
	case "next":
		if l.next.Contains(x, y) {
			s.ShowNext()
		}
	case "task":
		s.menuOpen = false
	}
}

func (s *Screen) StatusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "self-test ok: display=%s keyboard=%s mouse=%s disk=%s", s.Info.Display, s.Info.Keyboard, s.Info.Mouse, s.Info.Disk)
	return b.String()
}
