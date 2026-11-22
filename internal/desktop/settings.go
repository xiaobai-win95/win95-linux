package desktop

import (
	"fmt"
	"runtime"
	"time"

	"win95linux/internal/ui"
)

const settingsVersion = "26.5"

type settingsPage int

const (
	settingsLanguage settingsPage = iota
	settingsDisplay
	settingsFonts
	settingsTheme
	settingsTime
	settingsRegion
	settingsAbout
	settingsLicense
	settingsPageCount
)

var settingsPageNames = []string{"语言", "显示", "字体", "主题", "时间", "地区", "关于", "许可协议"}
var settingsPageIcons = []ui.IconKind{
	ui.IcoText, ui.IcoComputer, ui.IcoText, ui.IcoPaint,
	ui.IcoCalendar, ui.IcoComputer, ui.IcoInfo, ui.IcoText,
}

type SettingsApp struct {
	Base
	d   *Desktop
	win *Window

	page  settingsPage
	press string

	navRects      []ui.Rect
	langRects     []ui.Rect
	themeRects    []ui.Rect
	zoneBox       ui.Rect
	zoneScroll    VScroll
	dateRects     []ui.Rect
	clock12R      ui.Rect
	displayBtn    ui.Rect
	regionBox     ui.Rect
	regionScroll  VScroll
	regionRects   []ui.Rect
	licenseBox    ui.Rect
	licenseScroll VScroll
	closeBtn      ui.Rect
	jumpLicense   ui.Rect
	jumpTime      ui.Rect
}

func (d *Desktop) openSettings() {
	for _, w := range d.windows {
		if _, ok := w.App.(*SettingsApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &SettingsApp{d: d}
	a.win = d.Open(a)
	a.win.X = (d.W - a.win.W) / 2
	a.win.Y = (d.taskbarTop() - a.win.H) / 2
	if a.win.Y < 8 {
		a.win.Y = 8
	}
}

func (a *SettingsApp) Title() string           { return "设置中心" }
func (a *SettingsApp) Icon() ui.IconKind       { return ui.IcoSetup }
func (a *SettingsApp) DefaultSize() (int, int) { return 800, 560 }
func (a *SettingsApp) MinSize() (int, int)     { return 800, 560 }
func (a *SettingsApp) Resizable() bool         { return false }

func (a *SettingsApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	c.FillRect(client, ui.ColFace)
	f := ui.DefaultFont()

	header := ui.Rect{client.X, client.Y, client.W, 38}
	c.FillRect(header, ui.ColFace)
	c.HLine(header.X, header.Bottom()-2, header.W, ui.ColShadow)
	c.HLine(header.X, header.Bottom()-1, header.W, ui.ColWhite)
	ui.DrawIcon32(c, ui.IcoSetup, header.X+10, header.Y+3, false)
	f.TextBold(c, header.X+50, header.Y+4, d.tr("设置中心"), ui.ColText)
	f.Text(c, header.X+50, header.Y+21, settingsVersion+" · Windows 95 Linux", ui.ColShadow)

	navX := client.X + 10
	navY := client.Y + 48
	navW := 150
	a.navRects = make([]ui.Rect, int(settingsPageCount))
	for i := 0; i < int(settingsPageCount); i++ {
		r := ui.Rect{navX, navY + i*34, navW, 30}
		a.navRects[i] = r
		selected := settingsPage(i) == a.page
		if selected {
			c.FillRect(r, ui.ColNavy)
			c.HLine(r.X, r.Y, r.W, ui.ColNavyLight)
		} else {
			c.FillRect(r, ui.ColFace)
		}
		icon := settingsPageIcons[i]
		ui.DrawIcon16(c, icon, r.X+7, r.Y+7)
		col := ui.ColText
		if selected {
			col = ui.ColWhite
		}
		f.Text(c, r.X+30, r.Y+7, d.tr(settingsPageNames[i]), col)
	}

	body := ui.Rect{navX + navW + 12, client.Y + 48, client.Right() - (navX + navW + 22), client.Bottom() - 48 - 44}
	c.FillRect(body, ui.ColFace)
	c.Raised(body)
	inner := body.Inset(12)

	switch a.page {
	case settingsLanguage:
		a.paintLanguage(d, c, f, inner)
	case settingsDisplay:
		a.paintDisplay(d, c, f, inner)
	case settingsFonts:
		a.paintFonts(d, c, f, inner)
	case settingsTheme:
		a.paintTheme(d, c, f, inner)
	case settingsTime:
		a.paintTime(d, c, f, inner)
	case settingsRegion:
		a.paintRegion(d, c, f, inner)
	case settingsAbout:
		a.paintAbout(d, c, f, inner)
	case settingsLicense:
		a.paintLicense(d, c, f, inner)
	}

	a.closeBtn = ui.Rect{client.Right() - 10 - 88, client.Bottom() - 38, 88, 28}
	ui.Button(c, a.closeBtn, d.tr("关闭"), a.press == "close", true)
	f.TextClip(c, client.X+12, client.Bottom()-30, a.closeBtn.X-client.X-24,
		d.tr("设置会保存在 /root/.config/win95/desktop.json"), ui.ColShadow)
}

func drawRadio(c *ui.Canvas, r ui.Rect, selected bool) {
	c.FillRect(r, ui.ColField)
	c.Sunken(r)
	if selected {
		c.FillRect(r.Inset(4), ui.ColNavy)
	}
}

func (a *SettingsApp) paintLanguage(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("界面语言"), ui.ColText)
	f.TextClip(c, inner.X, inner.Y+22, inner.W, d.tr("选择界面语言，应用后立即生效。"), ui.ColShadow)
	a.langRects = make([]ui.Rect, len(desktopLanguages))
	y := inner.Y + 48
	for i, l := range desktopLanguages {
		r := ui.Rect{inner.X, y, inner.W - 4, 30}
		a.langRects[i] = r
		if l.Code == d.lang {
			c.FillRect(r, ui.ColWhite)
			c.Sunken(r)
		}
		drawRadio(c, ui.Rect{r.X + 8, r.Y + 7, 16, 16}, l.Code == d.lang)
		f.Text(c, r.X+32, r.Y+2, l.Name, ui.ColText)
		f.TextClip(c, r.X+32, r.Y+17, r.W-40, l.English, ui.ColShadow)
		y += 32
	}
	f.TextClip(c, inner.X, inner.Bottom()-20, inner.W,
		d.tr("语言设置会立即刷新桌面图标、开始菜单和任务栏。"), ui.ColShadow)
}

func (a *SettingsApp) paintDisplay(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("显示"), ui.ColText)
	lines := []string{
		fmt.Sprintf("分辨率：%d × %d", d.W, d.H),
		fmt.Sprintf("颜色深度：32 位（XRGB）"),
		fmt.Sprintf("桌面背景：%s", backgroundStyles[d.background]),
		fmt.Sprintf("屏幕保护：%s，等待 %d 分钟", saverNames[d.saverStyle], int(d.saverWait/time.Minute)),
	}
	y := inner.Y + 34
	for _, line := range lines {
		f.Text(c, inner.X+6, y, line, ui.ColText)
		y += 24
	}
	a.displayBtn = ui.Rect{inner.X + 6, y + 18, 200, 30}
	ui.Button(c, a.displayBtn, d.tr("打开显示属性…"), a.press == "display", true)
	f.TextClip(c, inner.X, y+66, inner.W,
		"显示属性可调整桌面背景与屏幕保护程序。分辨率由 framebuffer 决定。", ui.ColShadow)
}

func (a *SettingsApp) paintFonts(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("字体预览"), ui.ColText)
	samples := []string{
		"Windows 95 Linux 26.5 — 中文 天地玄黄，宇宙洪荒。",
		"English: The quick brown fox jumps over the lazy dog.",
		"Ελληνικά · Русский · עברית · العربية · हिन्दी · ไทย",
		"日本語：いろはにほへと ちりぬるを",
		"한국어: 다람쥐 헌 쳇바퀴에 타고파",
	}
	y := inner.Y + 28
	for _, s := range samples {
		f.TextClip(c, inner.X+4, y, inner.W-8, s, ui.ColText)
		y += 24
	}
	y += 6
	ui.GroupBox(c, ui.Rect{inner.X, y, inner.W, 152}, d.tr("字体覆盖"))
	coverage := []string{
		"拉丁字母 / 希腊字母 / 西里尔字母",
		"希伯来文 / 阿拉伯文 / 天城文 / 泰文",
		"日文假名 / 韩文谚文 / 中日韩统一表意文字",
		"符号、箭头、制表符与全角字符",
	}
	yy := y + 24
	for _, s := range coverage {
		f.Text(c, inner.X+16, yy, "· "+s, ui.ColText)
		yy += 22
	}
	f.Text(c, inner.X+16, yy+2, "共 35,654 个点阵字形，16 px 等宽排版。", ui.ColShadow)
	f.TextClip(c, inner.X, inner.Bottom()-22, inner.W,
		d.tr("世界语言与字体已全部安装。"), ui.ColShadow)
}

func (a *SettingsApp) paintTheme(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("主题"), ui.ColText)
	f.TextClip(c, inner.X, inner.Y+22, inner.W, d.tr("选择主题，应用后立即生效。"), ui.ColShadow)
	a.themeRects = make([]ui.Rect, len(ui.Palettes))
	y := inner.Y + 48
	for i, p := range ui.Palettes {
		r := ui.Rect{inner.X, y, inner.W - 4, 44}
		a.themeRects[i] = r
		if i == d.theme {
			c.FillRect(r, ui.ColWhite)
			c.Sunken(r)
		} else {
			c.FillRect(r, ui.ColFace)
			c.Raised(r)
		}
		drawRadio(c, ui.Rect{r.X + 8, r.Y + 14, 16, 16}, i == d.theme)
		f.Text(c, r.X+32, r.Y+5, p.Name, ui.ColText)
		sw := []uint32{p.Desktop, p.Face, p.Navy, p.NavyLight, p.Yellow}
		for j, col := range sw {
			sr := ui.Rect{r.X + 32 + j*30, r.Y + 24, 26, 14}
			c.FillRect(sr, col)
			c.Sunken(sr)
		}
		f.Text(c, r.X+32+len(sw)*30+8, r.Y+22, d.tr("桌面与窗口颜色"), ui.ColShadow)
		y += 48
	}
}

func (a *SettingsApp) paintTime(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("时间"), ui.ColText)
	now := time.Now().In(d.clockLoc)
	preview := ui.Rect{inner.X, inner.Y + 24, inner.W, 44}
	c.FillRect(preview, ui.ColField)
	c.Sunken(preview)
	f.TextBold(c, preview.X+10, preview.Y+5, d.formatDate(now)+"  "+d.formatClock(now), ui.ColText)
	f.TextClip(c, preview.X+10, preview.Y+24, preview.W-20, d.clockZone, ui.ColShadow)

	listW := (inner.W - 12) / 2
	box := ui.Rect{inner.X, inner.Y + 82, listW, inner.H - 104}
	a.zoneBox = box
	f.Text(c, box.X, box.Y-18, d.tr("时区"), ui.ColText)
	c.FillRect(box, ui.ColField)
	c.Sunken(box)
	innerBox := box.Inset(2)
	scrollW := 16
	rows := (innerBox.H - 2) / 22
	if rows < 1 {
		rows = 1
	}
	a.zoneScroll.Total = len(worldZones)
	a.zoneScroll.Visible = rows
	a.zoneScroll.Clamp()
	y := innerBox.Y + 1
	for i := a.zoneScroll.Pos; i < len(worldZones) && y+21 <= innerBox.Bottom(); i++ {
		r := ui.Rect{innerBox.X + 1, y, innerBox.W - scrollW - 2, 21}
		selected := worldZones[i].ID == d.clockZone
		if selected {
			c.FillRect(r, ui.ColNavy)
		}
		col := ui.ColText
		if selected {
			col = ui.ColWhite
		}
		f.TextClip(c, r.X+5, r.Y+3, r.W-8, d.zoneLabel(worldZones[i]), col)
		y += 22
	}
	a.zoneScroll.Draw(c, ui.Rect{innerBox.Right() - scrollW - 1, innerBox.Y + 1, scrollW, innerBox.H - 2})

	rightX := box.Right() + 12
	f.Text(c, rightX, box.Y-18, d.tr("日期格式"), ui.ColText)
	a.dateRects = make([]ui.Rect, len(dateFormatChoices))
	yy := box.Y
	for i, ch := range dateFormatChoices {
		r := ui.Rect{rightX, yy, inner.Right() - rightX, 24}
		a.dateRects[i] = r
		drawRadio(c, ui.Rect{r.X + 2, r.Y + 4, 16, 16}, d.dateFmt == i)
		f.Text(c, r.X+26, r.Y+4, ch.Label, ui.ColText)
		f.Text(c, r.X+26+f.Width(ch.Label)+10, r.Y+4, time.Date(2026, 11, 8, 15, 4, 0, 0, time.UTC).Format(ch.Layout), ui.ColShadow)
		yy += 26
	}
	a.clock12R = ui.Rect{rightX, yy + 8, inner.Right() - rightX, 24}
	drawRadio(c, ui.Rect{a.clock12R.X + 2, a.clock12R.Y + 4, 16, 16}, d.clock12)
	f.Text(c, a.clock12R.X+26, a.clock12R.Y+4, d.tr("24 小时制"), ui.ColText)
	if !d.clock12 {
		f.Text(c, a.clock12R.X+26+f.Width(d.tr("24 小时制"))+8, a.clock12R.Y+4, "✓ 24h", ui.ColShadow)
	} else {
		f.Text(c, a.clock12R.X+26+f.Width(d.tr("24 小时制"))+8, a.clock12R.Y+4, "AM / PM", ui.ColShadow)
	}
	f.TextClip(c, rightX, a.clock12R.Bottom()+8, inner.Right()-rightX,
		d.tr("当前时间")+"："+d.dateSample(), ui.ColShadow)
}

func (a *SettingsApp) paintRegion(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("区域预设"), ui.ColText)
	f.TextClip(c, inner.X, inner.Y+22, inner.W, d.tr("此区域将设置语言、时区和日期格式。"), ui.ColShadow)
	box := ui.Rect{inner.X, inner.Y + 50, inner.W, inner.H - 72}
	a.regionBox = box
	c.FillRect(box, ui.ColField)
	c.Sunken(box)
	innerBox := box.Inset(2)
	scrollW := 16
	rows := (innerBox.H - 2) / 26
	if rows < 1 {
		rows = 1
	}
	a.regionScroll.Total = len(regionPresets)
	a.regionScroll.Visible = rows
	a.regionScroll.Clamp()
	a.regionRects = make([]ui.Rect, len(regionPresets))
	y := innerBox.Y + 1
	for i := a.regionScroll.Pos; i < len(regionPresets) && y+25 <= innerBox.Bottom(); i++ {
		r := ui.Rect{innerBox.X + 1, y, innerBox.W - scrollW - 2, 25}
		a.regionRects[i] = r
		selected := regionPresets[i].Lang == d.lang && regionPresets[i].Zone == d.clockZone
		if selected {
			c.FillRect(r, ui.ColNavy)
		}
		col := ui.ColText
		if selected {
			col = ui.ColWhite
		}
		rp := regionPresets[i]
		f.Text(c, r.X+8, r.Y+4, rp.Name, col)
		meta := fmt.Sprintf("%s · %s · %s", rp.Lang, rp.Zone, dateFormatChoices[rp.DateFmt].Label)
		metaCol := ui.ColShadow
		if selected {
			metaCol = ui.ColWhite
		}
		f.TextClip(c, r.X+150, r.Y+4, r.W-158, meta, metaCol)
		y += 26
	}
	a.regionScroll.Draw(c, ui.Rect{innerBox.Right() - scrollW - 1, innerBox.Y + 1, scrollW, innerBox.H - 2})
}

func (a *SettingsApp) paintAbout(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	ui.DrawIcon32(c, ui.IcoComputer, inner.X+4, inner.Y+2, false)
	f.TextBold(c, inner.X+48, inner.Y+2, "Windows 95 Linux 中文版", ui.ColText)
	f.Text(c, inner.X+48, inner.Y+22, settingsVersion+" · ARM64 / UEFI", ui.ColShadow)
	y := inner.Y + 62
	rows := [][2]string{
		{"版本", settingsVersion},
		{"系统内核", runtime.GOOS + " / " + runtime.GOARCH},
		{"构建工具链", runtime.Version()},
		{"系统基础", "Alpine Linux（musl）+ Arch Linux ARM（glibc 子系统）"},
		{"处理器", fmt.Sprintf("%d 核 · CPU %d%%", runtime.NumCPU(), d.cpuPct)},
		{"内存占用", fmt.Sprintf("%d%%", d.memPct)},
		{"界面语言", d.languageDef().Name + " / " + d.languageDef().English},
		{"时区", d.clockZone + " · " + d.formatDate(time.Now()) + " " + d.formatClock(time.Now())},
		{"浏览器", "自研 HTML/CSS 渲染 + goja JavaScript 引擎"},
		{"图形", "framebuffer 32bpp · X11 嵌套代理（Xvfb :1）"},
	}
	for _, row := range rows {
		f.Text(c, inner.X+4, y, row[0]+"：", ui.ColShadow)
		f.TextClip(c, inner.X+124, y, inner.W-128, row[1], ui.ColText)
		y += 24
	}
	f.Text(c, inner.X+4, y+6, d.tr("许可证")+"：Apache License 2.0", ui.ColText)
	a.jumpLicense = ui.Rect{inner.X + 4, y + 28, 120, 26}
	a.jumpTime = ui.Rect{inner.X + 132, y + 28, 120, 26}
	ui.Button(c, a.jumpLicense, d.tr("许可协议"), a.press == "jumpLicense", true)
	ui.Button(c, a.jumpTime, d.tr("时间"), a.press == "jumpTime", true)
	f.TextClip(c, inner.X, inner.Bottom()-20, inner.W, "Copyright © 2026 Windows 95 Linux contributors", ui.ColShadow)
}

func (a *SettingsApp) paintLicense(d *Desktop, c *ui.Canvas, f *ui.Font, inner ui.Rect) {
	f.TextBold(c, inner.X, inner.Y, d.tr("许可协议"), ui.ColText)
	box := ui.Rect{inner.X, inner.Y + 26, inner.W, inner.H - 44}
	a.licenseBox = box
	c.FillRect(box, ui.ColField)
	c.Sunken(box)
	lines := wrapText(a.licenseText(), box.W-24)
	innerBox := box.Inset(2)
	scrollW := 16
	rows := (innerBox.H - 6) / 18
	if rows < 1 {
		rows = 1
	}
	a.licenseScroll.Total = len(lines)
	a.licenseScroll.Visible = rows
	a.licenseScroll.Clamp()
	y := innerBox.Y + 3
	for i := a.licenseScroll.Pos; i < len(lines) && y+18 <= innerBox.Bottom(); i++ {
		f.Text(c, innerBox.X+8, y, lines[i], ui.ColText)
		y += 18
	}
	a.licenseScroll.Draw(c, ui.Rect{innerBox.Right() - scrollW - 1, innerBox.Y + 1, scrollW, innerBox.H - 2})
}

func (a *SettingsApp) licenseText() string {
	return "Windows 95 Linux 26.5\nCopyright © 2026 Windows 95 Linux contributors\n\n" +
		apacheLicense + "\n\n第三方组件\n" +
		"————————————————————————————\n" +
		"Go 语言运行时与标准库：BSD 3-Clause License，Copyright © The Go Authors。\n" +
		"github.com/dop251/goja：MIT License，JavaScript 引擎。\n" +
		"golang.org/x/net：BSD 3-Clause License。\n" +
		"GNU Unifont：GNU GPL v2 或更高版本（带字体嵌入例外），用于世界语言点阵字形。\n" +
		"文泉驿点阵宋体（WenQuanYi）：Apache License 2.0 / GPL v2，用于中日韩字形。\n" +
		"Alpine Linux：MIT License 及各软件包自身许可证。\n" +
		"Arch Linux ARM：GPL 及各软件包自身许可证。\n" +
		"Mesa、Xorg、XFCE：各自的开源许可证。\n\n" +
		"本程序按“原样”提供，不附带任何明示或暗示的担保。\n"
}

func (a *SettingsApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		scrollPage := func(s *VScroll, box ui.Rect) bool {
			if !box.Contains(ev.X, ev.Y) {
				return false
			}
			delta := -ev.Wheel
			if delta == 0 {
				delta = 1
			}
			if delta > 0 {
				s.ScrollBy(3)
			} else {
				s.ScrollBy(-3)
			}
			d.Invalidate()
			return true
		}
		switch a.page {
		case settingsTime:
			scrollPage(&a.zoneScroll, a.zoneBox)
		case settingsRegion:
			scrollPage(&a.regionScroll, a.regionBox)
		case settingsLicense:
			scrollPage(&a.licenseScroll, a.licenseBox)
		}
		return
	}

	if ev.Kind == MouseMove {
		if changed, _ := a.zoneScroll.Mouse(ev, ui.Rect{a.zoneBox.Right() - 17, a.zoneBox.Y + 3, 16, a.zoneBox.H - 6}); changed {
			d.Invalidate()
		}
		if changed, _ := a.regionScroll.Mouse(ev, ui.Rect{a.regionBox.Right() - 17, a.regionBox.Y + 3, 16, a.regionBox.H - 6}); changed {
			d.Invalidate()
		}
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		for i, r := range a.navRects {
			if r.Contains(ev.X, ev.Y) {
				a.page = settingsPage(i)
				d.Invalidate()
				return
			}
		}
		if a.closeBtn.Contains(ev.X, ev.Y) {
			a.press = "close"
			return
		}
		switch a.page {
		case settingsLanguage:
			for i, r := range a.langRects {
				if r.Contains(ev.X, ev.Y) {
					d.applyLanguage(desktopLanguages[i].Code)
					d.Invalidate()
					return
				}
			}
		case settingsTheme:
			for i, r := range a.themeRects {
				if r.Contains(ev.X, ev.Y) {
					d.theme = i
					ui.ApplyPalette(ui.Palettes[i])
					d.savePrefs()
					d.Invalidate()
					return
				}
			}
		case settingsTime:
			zoneScrollRect := ui.Rect{a.zoneBox.Right() - 17, a.zoneBox.Y + 3, 16, a.zoneBox.H - 6}
			if zoneScrollRect.Contains(ev.X, ev.Y) {
				if changed, _ := a.zoneScroll.Mouse(ev, zoneScrollRect); changed {
					d.Invalidate()
				}
				return
			}
			if a.zoneBox.Contains(ev.X, ev.Y) {
				idx := a.zoneScroll.Pos + (ev.Y-a.zoneBox.Y-3)/22
				if idx >= 0 && idx < len(worldZones) {
					if loc, err := time.LoadLocation(worldZones[idx].ID); err == nil {
						d.clockLoc = loc
						d.clockZone = worldZones[idx].ID
						d.clock = d.formatClock(time.Now())
						d.savePrefs()
						d.Invalidate()
					}
				}
				return
			}
			for i, r := range a.dateRects {
				if r.Contains(ev.X, ev.Y) {
					d.dateFmt = i
					d.clock = d.formatClock(time.Now())
					d.savePrefs()
					d.Invalidate()
					return
				}
			}
			if a.clock12R.Contains(ev.X, ev.Y) {
				d.clock12 = !d.clock12
				d.clock = d.formatClock(time.Now())
				d.savePrefs()
				d.Invalidate()
				return
			}
		case settingsRegion:
			if a.regionBox.Contains(ev.X, ev.Y) {
				idx := a.regionScroll.Pos + (ev.Y-a.regionBox.Y-3)/26
				if idx >= 0 && idx < len(regionPresets) {
					d.applyRegion(idx)
					d.Invalidate()
				}
				return
			}
		case settingsDisplay:
			if a.displayBtn.Contains(ev.X, ev.Y) {
				a.press = "display"
				return
			}
		case settingsAbout:
			if a.jumpLicense.Contains(ev.X, ev.Y) {
				a.press = "jumpLicense"
				return
			}
			if a.jumpTime.Contains(ev.X, ev.Y) {
				a.press = "jumpTime"
				return
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "close":
			if a.closeBtn.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		case "display":
			if a.displayBtn.Contains(ev.X, ev.Y) {
				d.openDisplayProperties()
			}
		case "jumpLicense":
			if a.jumpLicense.Contains(ev.X, ev.Y) {
				a.page = settingsLicense
				a.licenseScroll.Pos = 0
				d.Invalidate()
			}
		case "jumpTime":
			if a.jumpTime.Contains(ev.X, ev.Y) {
				a.page = settingsTime
				d.Invalidate()
			}
		}
	}
}

func (a *SettingsApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Left", "Up":
		if a.page > 0 {
			a.page--
			d.Invalidate()
		}
		return true
	case "Right", "Down":
		if a.page < settingsPageCount-1 {
			a.page++
			d.Invalidate()
		}
		return true
	case "PageUp":
		a.licenseScroll.ScrollBy(-a.licenseScroll.Visible)
		a.regionScroll.ScrollBy(-a.regionScroll.Visible)
		d.Invalidate()
		return true
	case "PageDown":
		a.licenseScroll.ScrollBy(a.licenseScroll.Visible)
		a.regionScroll.ScrollBy(a.regionScroll.Visible)
		d.Invalidate()
		return true
	}
	return false
}

// settingsSummary is shown in the welcome/about dialogs.
func (d *Desktop) settingsSummary() string {
	return fmt.Sprintf("%s · %s · %s", d.languageDef().Name, d.clockZone, d.formatDate(time.Now()))
}
