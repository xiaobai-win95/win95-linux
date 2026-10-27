package desktop

import (
	"fmt"
	"strings"
	"time"

	"win95linux/internal/ui"
)

type diskInfo struct {
	path      string
	model     string
	sizeBytes int64
	removable bool
	installer bool
	hasTable  bool
	parts     int
}

type installProgress struct {
	step int
	pct  int
	msg  string
}

type installResult struct {
	rootUUID string
}

func (d diskInfo) selectable() bool {
	return !d.installer && d.sizeBytes >= 1<<30
}

func (d diskInfo) modelName() string {
	if strings.TrimSpace(d.model) != "" {
		return strings.TrimSpace(d.model)
	}
	return "虚拟磁盘"
}

const (
	instChoose = iota
	instRunning
	instDone
	instFailed
)

var installStepNames = []string{
	"准备目标磁盘",
	"写入 GPT 分区表",
	"格式化 EFI 分区",
	"格式化 Linux 分区",
	"解压系统文件",
	"安装内核与引导程序",
	"完成",
}

type InstallerApp struct {
	Base
	d      *Desktop
	win    *Window
	disks  []diskInfo
	sel    int
	scroll VScroll

	stage     int
	step      int
	pct       int
	anim      int
	log       []string
	err       string
	target    diskInfo
	result    installResult
	lastClick time.Time
	lastRow   int

	btnStart, btnExit, btnRefresh, btnReboot, btnRetry ui.Rect
	rows                                               []ui.Rect
	listR                                              ui.Rect
	client                                             ui.Rect
}

func NewInstallerApp(d *Desktop) *InstallerApp {
	a := &InstallerApp{d: d, sel: -1, scroll: VScroll{Visible: 3}, lastRow: -1}
	a.refreshDisks(false)
	return a
}

func (a *InstallerApp) Title() string     { return "Windows 95 Linux 安装程序" }
func (a *InstallerApp) Icon() ui.IconKind { return ui.IcoSetup }

func (a *InstallerApp) DefaultSize() (int, int) { return 720, 520 }
func (a *InstallerApp) MinSize() (int, int)     { return 640, 470 }
func (a *InstallerApp) Resizable() bool         { return true }

func (a *InstallerApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "刷新磁盘列表(R)", Enabled: a.stage != instRunning, Action: func() { a.refreshDisks(true) }},
			{Separator: true},
			{Label: "退出安装程序(X)", Enabled: a.stage != instRunning, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "安装说明(A)", Enabled: true, Action: func() { d.showInstallHelp() }},
			{Label: "关于安装程序(A)", Enabled: true, Action: func() {
				d.MessageBox("关于安装程序", "Windows 95 Linux 安装程序\n版本 1.0\n\n将系统安装到本地硬盘，并建立 UEFI 启动项。\n目标磁盘会被重新分区。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *InstallerApp) refreshDisks(notify bool) {
	disks, err := listDisks()
	a.disks = disks
	a.scroll.Pos = 0
	if err != nil {
		a.err = err.Error()
		a.sel = -1
	} else {
		a.err = ""
		a.sel = -1
	}
	a.scroll.Total = len(a.disks)
	a.scroll.Clamp()
	if notify && a.d != nil {
		a.d.Invalidate()
	}
}

func (a *InstallerApp) RequestClose(d *Desktop, w *Window) bool {
	if a.stage == instRunning {
		d.MessageBox("安装程序", "安装正在进行中，不能关闭安装程序。\n\n请等待安装完成。", IconWarning, []string{"确定"}, nil)
		return false
	}
	return true
}

func (a *InstallerApp) Close() {}

func (a *InstallerApp) startInstall() {
	if a.stage == instRunning {
		return
	}
	if a.sel < 0 || a.sel >= len(a.disks) {
		a.d.MessageBox("安装程序", "请先选择目标磁盘。", IconWarning, []string{"确定"}, nil)
		return
	}
	target := a.disks[a.sel]
	if target.installer {
		a.d.MessageBox("安装程序", "不能把系统安装到安装盘本身。\n\n请选择另一块磁盘。", IconWarning, []string{"确定"}, nil)
		return
	}
	if target.sizeBytes < 1<<30 {
		a.d.MessageBox("安装程序", "目标磁盘空间不足。\n\n系统需要至少 1 GB 的磁盘空间。", IconWarning, []string{"确定"}, nil)
		return
	}
	if target.hasTable || target.parts > 0 {
		msg := fmt.Sprintf("所选磁盘 %s 上已有分区表（%d 个分区）。\n\n继续安装会清空该磁盘上的全部分区和数据，且无法恢复。\n\n确认要使用这块磁盘吗？",
			target.path, target.parts)
		a.d.MessageBox("磁盘已有数据", msg, IconWarning, []string{"继续", "取消"}, func(i int) {
			if i == 0 {
				a.confirmInstall(target)
			}
		})
		return
	}
	a.confirmInstall(target)
}

func (a *InstallerApp) confirmInstall(target diskInfo) {
	msg := fmt.Sprintf("即将把 Windows 95 Linux 安装到：\n\n%s（%s，%s）\n\n警告：目标磁盘上的所有分区和数据都会被永久删除！\n\n确实要继续吗？",
		target.path, target.modelName(), formatBytes(target.sizeBytes))
	a.d.MessageBox("确认安装", msg, IconWarning, []string{"开始安装", "取消"}, func(i int) {
		if i != 0 {
			return
		}
		a.target = target
		a.beginInstall()
	})
}

func (a *InstallerApp) beginInstall() {
	a.stage = instRunning
	a.step = 0
	a.pct = -1
	a.anim = 0
	a.err = ""
	a.log = nil
	a.appendLog("目标磁盘: " + a.target.path + " (" + formatBytes(a.target.sizeBytes) + ")")
	a.d.Invalidate()
	target := a.target
	go func() {
		res, err := installSystem(target, func(p installProgress) {
			a.d.Post(func() {
				if a.stage != instRunning {
					return
				}
				a.step = p.step
				a.pct = p.pct
				if p.msg != "" {
					a.appendLog(p.msg)
				}
				a.d.Invalidate()
			})
		})
		a.d.Post(func() {
			if err != nil {
				a.stage = instFailed
				a.err = err.Error()
				a.appendLog("安装失败: " + err.Error())
			} else {
				a.stage = instDone
				a.result = res
				a.step = 6
				a.pct = 100
				a.appendLog("系统安装完成。")
			}
			a.d.Invalidate()
		})
	}()
}

func (a *InstallerApp) appendLog(s string) {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		a.log = append(a.log, line)
	}
	if len(a.log) > 200 {
		a.log = a.log[len(a.log)-200:]
	}
}

func (a *InstallerApp) retry() {
	a.stage = instChoose
	a.err = ""
	a.log = nil
	a.pct = -1
	a.refreshDisks(false)
	a.d.Invalidate()
}

func (a *InstallerApp) Tick(d *Desktop, w *Window, now time.Time) {
	if a.stage == instRunning && a.pct < 0 {
		a.anim += 7
		if a.anim > 95 {
			a.anim = 0
		}
		d.Invalidate()
	}
}

func (a *InstallerApp) layout(client ui.Rect) {
	a.client = client
	f := ui.DefaultFont()
	_ = f
	bannerW := 150
	if client.W < 560 {
		bannerW = 118
	}
	x0 := client.X + bannerW + 20
	w := client.Right() - x0 - 16
	by := client.Bottom() - 40
	a.btnExit = ui.Rect{client.Right() - 122, by, 110, 28}
	a.btnStart = ui.Rect{a.btnExit.X - 118, by, 110, 28}
	a.btnRefresh = ui.Rect{x0, by, 100, 28}
	a.btnReboot = a.btnStart
	a.btnRetry = a.btnStart
	a.listR = ui.Rect{x0 + 12, client.Y + 108, w - 24, 152}
	a.scroll.Visible = 3
	rows := a.scroll.Visible
	a.rows = a.rows[:0]
	for i := 0; i < rows; i++ {
		a.rows = append(a.rows, ui.Rect{a.listR.X + 2, a.listR.Y + 2 + i*49, a.listR.W - 18, 47})
	}
}

func (a *InstallerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	a.layout(client)
	if a.scroll.Total != len(a.disks) {
		a.scroll.Total = len(a.disks)
		a.scroll.Clamp()
	}
	c.FillRect(client, ui.ColFace)
	bannerW := 150
	if client.W < 560 {
		bannerW = 118
	}
	a.drawBanner(c, ui.Rect{client.X, client.Y, bannerW, client.H})
	x0 := client.X + bannerW + 20
	right := client.Right() - 16
	f := ui.DefaultFont()
	switch a.stage {
	case instChoose:
		f.TextBold(c, x0, client.Y+14, "欢迎使用 Windows 95 Linux 安装程序", ui.ColText)
		lines := wrapText("安装程序将把 Windows 95 风格的中文 Linux 系统安装到本地硬盘。请选择一块目标磁盘。", right-x0)
		y := client.Y + 40
		for _, ln := range lines {
			f.Text(c, x0, y, ln, ui.ColText)
			y += 18
		}
		ui.GroupBox(c, ui.Rect{x0, client.Y + 88, right - x0, 180}, "目标磁盘(T)")
		c.FillRect(a.listR, ui.ColWhite)
		c.Sunken(a.listR)
		a.paintDiskList(c, f)
		scrollR := ui.Rect{a.listR.Right() - 15, a.listR.Y + 1, 14, a.listR.H - 2}
		a.scroll.Draw(c, scrollR)

		if a.err != "" {
			for i, ln := range wrapText(a.err, right-x0) {
				f.Text(c, x0, client.Y+278+floatY(i), ln, 0x800000)
			}
		} else if a.sel >= 0 && a.sel < len(a.disks) {
			dsk := a.disks[a.sel]
			status := "已选择 " + dsk.path + "，安装将清除该磁盘上的全部数据。"
			if dsk.installer {
				status = "该磁盘是安装盘，不能作为目标。"
			}
			f.TextClip(c, x0, client.Y+278, right-x0, status, ui.ColRed)
		} else {
			f.Text(c, x0, client.Y+278, "请选择一块可用的目标磁盘。", ui.ColRed)
		}
		f.Text(c, x0, client.Y+298, "提示：请先在虚拟机设置中添加一块新的 VirtIO 磁盘。", ui.ColShadow)

		ui.Button(c, a.btnRefresh, "刷新(R)", false, true)
		ui.Button(c, a.btnStart, "开始安装", false, a.sel >= 0 && a.sel < len(a.disks) && a.disks[a.sel].selectable())
		ui.Button(c, a.btnExit, "退出", false, true)
	case instRunning:
		title := "正在安装 Windows 95 Linux"
		f.TextBold(c, x0, client.Y+14, title, ui.ColText)
		step := a.step
		if step < 0 {
			step = 0
		}
		if step >= len(installStepNames) {
			step = len(installStepNames) - 1
		}
		f.Text(c, x0, client.Y+44, fmt.Sprintf("步骤 %d/%d：%s", step+1, len(installStepNames), installStepNames[step]), ui.ColText)
		pct := a.pct
		if pct < 0 {
			pct = a.anim
		}
		ui.ProgressBar(c, ui.Rect{x0, client.Y + 68, right - x0, 22}, pct)
		if a.pct >= 0 {
			f.TextRight(c, right, client.Y+70, fmt.Sprintf("%d%%", a.pct), ui.ColText)
		}
		logR := ui.Rect{x0, client.Y + 104, right - x0, client.Bottom() - client.Y - 124}
		c.FillRect(logR, ui.ColWhite)
		c.Sunken(logR)
		a.paintLog(c, f, logR, 6)
	case instDone:
		f.TextBold(c, x0, client.Y+14, "安装完成", ui.ColText)
		y := client.Y + 50
		for _, ln := range wrapText("Windows 95 Linux 已成功安装到目标磁盘。", right-x0) {
			f.Text(c, x0, y, ln, ui.ColText)
			y += 20
		}
		y += 8
		msg := "请关闭虚拟机，从虚拟机设置中移除安装映像，然后重新启动。系统将从目标硬盘启动。"
		for _, ln := range wrapText(msg, right-x0) {
			f.Text(c, x0, y, ln, ui.ColText)
			y += 18
		}
		y += 10
		if a.result.rootUUID != "" {
			f.TextClip(c, x0, y, right-x0, "根分区 PARTUUID: "+a.result.rootUUID, ui.ColShadow)
			y += 20
		}
		logR := ui.Rect{x0, y + 6, right - x0, client.Bottom() - y - 56}
		if logR.H > 20 {
			c.FillRect(logR, ui.ColWhite)
			c.Sunken(logR)
			a.paintLog(c, f, logR, 6)
		}
		ui.Button(c, a.btnReboot, "关闭计算机", false, true)
		ui.Button(c, a.btnExit, "退出", false, true)
	case instFailed:
		f.TextBold(c, x0, client.Y+14, "安装失败", ui.ColRed)
		y := client.Y + 46
		for _, ln := range wrapText(a.err, right-x0) {
			f.Text(c, x0, y, ln, ui.ColText)
			y += 18
		}
		logR := ui.Rect{x0, y + 8, right - x0, client.Bottom() - y - 56}
		if logR.H > 20 {
			c.FillRect(logR, ui.ColWhite)
			c.Sunken(logR)
			a.paintLog(c, f, logR, 6)
		}
		ui.Button(c, a.btnRetry, "返回", false, true)
		ui.Button(c, a.btnExit, "退出", false, true)
	}
}

func floatY(i int) int { return i * 18 }

func (a *InstallerApp) drawBanner(c *ui.Canvas, r ui.Rect) {
	c.TitleBar(r, 0x000040, 0x000080)
	ui.FlagLogo(c, r.X+18, r.Y+22, 32)
	f := ui.DefaultFont()
	f.TextBold(c, r.X+16, r.Y+68, "Windows 95", 0xFFFFFF)
	f.Text(c, r.X+16, r.Y+90, "Linux", 0xC0E0FF)
	f.Text(c, r.X+16, r.Y+110, "中文版", 0xFFFFFF)
	c.HLine(r.X+16, r.Y+136, r.W-32, 0x8080C0)
	f.Text(c, r.X+16, r.Y+152, "安 装 程 序", 0xC0E0FF)
	f.Text(c, r.X+16, r.Bottom()-40, "ARM64 / UEFI", 0x8080C0)
	f.Text(c, r.X+16, r.Bottom()-24, "版本 1.0", 0x8080C0)
}

func (a *InstallerApp) paintDiskList(c *ui.Canvas, f *ui.Font) {
	for i := 0; i < a.scroll.Visible; i++ {
		idx := a.scroll.Pos + i
		if idx >= len(a.disks) {
			break
		}
		dsk := a.disks[idx]
		row := a.rows[i]
		selected := idx == a.sel
		if selected {
			c.FillRect(row, ui.ColNavy)
		}
		col := ui.ColText
		sub := ui.ColShadow
		if selected {
			col = ui.ColWhite
			sub = 0xC0D0FF
		}
		avail := dsk.selectable()
		ui.Radio(c, row.X+6, row.Y+8, selected, avail)
		f.TextClip(c, row.X+24, row.Y+5, row.W-30, dsk.modelName(), col)
		info := fmt.Sprintf("%s · %s", dsk.path, formatBytes(dsk.sizeBytes))
		if dsk.installer {
			info += " · 安装盘（不可选）"
		} else if !avail {
			info += " · 空间不足"
		} else if dsk.removable {
			info += " · 可移动设备"
		}
		f.TextClip(c, row.X+24, row.Y+24, row.W-30, info, sub)
		if i < a.scroll.Visible-1 {
			c.HLine(row.X, row.Bottom()+1, row.W, 0xE0E0E0)
		}
	}
}

func (a *InstallerApp) paintLog(c *ui.Canvas, f *ui.Font, r ui.Rect, pad int) {
	lineH := 16
	maxLines := (r.H - 2*pad) / lineH
	if maxLines < 1 {
		return
	}
	start := 0
	if len(a.log) > maxLines {
		start = len(a.log) - maxLines
	}
	y := r.Y + pad
	for _, ln := range a.log[start:] {
		f.TextClip(c, r.X+pad, y, r.W-2*pad, ln, ui.ColText)
		y += lineH
	}
}

func (a *InstallerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	if a.stage == instRunning {
		return
	}
	if ev.Kind == MouseWheel {
		if a.stage == instChoose && a.listR.Contains(ev.X, ev.Y) {
			a.scroll.ScrollBy(-ev.Wheel)
			a.d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseDown {
		switch a.stage {
		case instChoose:
			switch {
			case a.btnRefresh.Contains(ev.X, ev.Y):
				a.refreshDisks(true)
				return
			case a.btnStart.Contains(ev.X, ev.Y):
				a.startInstall()
				return
			case a.btnExit.Contains(ev.X, ev.Y):
				a.d.CloseWindow(w)
				return
			}
			scrollR := ui.Rect{a.listR.Right() - 15, a.listR.Y + 1, 14, a.listR.H - 2}
			if scrollR.Contains(ev.X, ev.Y) {
				if changed, _ := a.scroll.Mouse(ev, scrollR); changed {
					a.d.Invalidate()
				}
				return
			}
			if a.listR.Contains(ev.X, ev.Y) {
				rows := a.scroll.Visible
				rowIdx := (ev.Y - a.listR.Y - 2) / 49
				if rowIdx >= 0 && rowIdx < rows {
					idx := a.scroll.Pos + rowIdx
					if idx >= 0 && idx < len(a.disks) {
						dsk := a.disks[idx]
						if dsk.installer {
							a.d.MessageBox("安装程序", "这是安装程序所在的磁盘，不能作为安装目标。\n\n请选择其他磁盘。", IconWarning, []string{"确定"}, nil)
							return
						}
						a.sel = idx
						now := time.Now()
						if a.lastRow == idx && now.Sub(a.lastClick) < 450*time.Millisecond && dsk.selectable() {
							a.lastRow = -1
							a.startInstall()
							return
						}
						a.lastRow = idx
						a.lastClick = now
						a.d.Invalidate()
						return
					}
				}
			}
		case instDone:
			if a.btnReboot.Contains(ev.X, ev.Y) {
				if d.Host.PowerOff != nil {
					d.Host.PowerOff()
				} else {
					d.MessageBox("提示", "预览模式无法关闭计算机。", IconInfo, []string{"确定"}, nil)
				}
				return
			}
			if a.btnExit.Contains(ev.X, ev.Y) {
				a.d.CloseWindow(w)
				return
			}
		case instFailed:
			if a.btnRetry.Contains(ev.X, ev.Y) {
				a.retry()
				return
			}
			if a.btnExit.Contains(ev.X, ev.Y) {
				a.d.CloseWindow(w)
				return
			}
		}
	}
}

func (a *InstallerApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	a.client = client
}
