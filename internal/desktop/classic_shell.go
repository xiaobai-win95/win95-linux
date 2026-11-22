package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"win95linux/internal/ui"
)

// Open a file with the same associations the file manager uses.
func (d *Desktop) openFileDefault(path string) {
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)
	switch {
	case ext == ".html" || ext == ".htm" || ext == ".xhtml" || ext == ".shtml":
		d.openBrowser("file://" + path)
	case ext == ".png" || ext == ".bmp" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif":
		d.openImageViewer(path)
	case ext == ".sh":
		d.runScript(path, "/bin/sh")
	case ext == ".bash":
		d.runScript(path, "/bin/bash")
	case ext == ".exe" || ext == ".msi" || ext == ".bat" || ext == ".cmd":
		d.openWineProgram(path)
	case officeFileModule(base) != "":
		d.openOfficeFile(path)
	case fmTextType(base):
		d.openNotepad(path)
	default:
		d.MessageBox("打开文件", "无法打开“"+base+"”。\n\n没有与此文件类型关联的程序。", IconInfo, []string{"确定"}, nil)
	}
}

type onlineServiceApp struct {
	Base
	d   *Desktop
	win *Window

	mode int

	user, pass []rune
	focus      int

	stage     int
	dialStart time.Time
	progress  float64
	sel       int
	status    string
	press     string

	userR, passR, btnConnect, btnCancel      ui.Rect
	btnMail, btnGames, btnBrowser, btnHangup ui.Rect
	rows                                     []ui.Rect
}

func (a *onlineServiceApp) Title() string {
	switch a.mode {
	case 0:
		return "AOL & Internet 免费试用"
	case 1:
		return "AOL 3.0i（含 Internet Explorer 4.0）"
	case 2:
		return "AOL Canada 3.0i（含 IE 4.0）"
	default:
		return "The Microsoft Network"
	}
}

func (a *onlineServiceApp) Icon() ui.IconKind {
	if a.mode == 3 {
		return ui.IcoMSN
	}
	return ui.IcoOnline
}

func (a *onlineServiceApp) DefaultSize() (int, int) { return 640, 440 }
func (a *onlineServiceApp) MinSize() (int, int)     { return 560, 400 }
func (a *onlineServiceApp) Resizable() bool         { return true }

func (a *onlineServiceApp) brand() string {
	switch a.mode {
	case 2:
		return "America Online Canada"
	case 3:
		return "The Microsoft Network"
	default:
		return "America Online"
	}
}

func (a *onlineServiceApp) accent() uint32 {
	switch a.mode {
	case 2:
		return 0x8B0000
	case 3:
		return 0x005A9C
	default:
		return 0x00008B
	}
}

func (a *onlineServiceApp) phone() string {
	switch a.mode {
	case 2:
		return "1-888-265-4357"
	case 3:
		return "1-800-386-5550"
	default:
		return "1-800-827-6364"
	}
}

func (a *onlineServiceApp) channels() []string {
	if a.mode == 3 {
		return []string{"MSN Today", "电子邮件", "聊天室", "下载中心", "在线服务"}
	}
	return []string{"今日焦点", "电子邮件", "新闻", "游戏", "在线服务"}
}

func (a *onlineServiceApp) channelText(i int) []string {
	brand := a.brand()
	if a.mode == 3 {
		switch i {
		case 0:
			return []string{"欢迎来到 " + brand + "！", "", "这是 1996 年的 MSN 体验：拨号、聊天室、", "下载中心和 MSN Today 每日新闻。", "", "当前连接：56.6 Kbps（离线演示）"}
		case 1:
			return []string{"MSN 电子邮件", "", "点击下方“打开邮件”查看收件箱。"}
		case 2:
			return []string{"MSN 聊天室", "", "正在列出可用聊天室…", "#Windows95 · #Netscape · #Macintosh", "", "（聊天服务器未连接：离线演示）"}
		case 3:
			return []string{"MSN 下载中心", "", "热门下载：", "· Netscape Navigator 4.8", "· Internet Explorer 4.0", "· WinZip 6.3", "", "提示：请从“开始 → 在线服务”安装 Netscape。"}
		default:
			return []string{"在线服务", "", "AOL、MSN、Netscape 与 Internet Explorer", "都可以从“在线服务”窗口打开。"}
		}
	}
	switch i {
	case 0:
		return []string{"欢迎回来，" + string(a.user) + "！", "", "今天的头条：Windows 95 Linux 26.5 发布，", "内置极速搜索、聚光灯、VNC 与 DOOM 95。", "", "免费试用剩余：49 小时 58 分（离线演示）"}
	case 1:
		return []string{"AOL 电子邮件", "", "您有 3 封未读邮件。", "点击下方“打开邮件”进入收件箱。"}
	case 2:
		return []string{"今日新闻", "", "·  Netscape Navigator 4.8 正式发布", "·  Microsoft 发布 Internet Explorer 4.0", "·  Y2K 问题提醒：请检查您的 BIOS 日期"}
	case 3:
		return []string{"AOL 游戏频道", "", "纸牌、扫雷、三维弹球、方块世界 95，", "以及全新的 DOOM 95 光线投射迷宫。", "", "点击下方“打开游戏”开始。"}
	default:
		return []string{"在线服务", "", "AOL Canada、MSN、Netscape Navigator…", "全部可以在“在线服务”窗口中打开。"}
	}
}

func (d *Desktop) openAOL(mode int) {
	a := &onlineServiceApp{d: d, mode: mode, user: []rune("win95user"), focus: 0,
		status: "输入用户名和密码，然后点击“连接”。"}
	a.win = d.Open(a)
}

func (d *Desktop) openMSN() {
	d.openAOL(3)
}

func (a *onlineServiceApp) connect(d *Desktop) {
	if len(a.user) == 0 {
		a.status = "请输入用户名。"
		a.focus = 0
		d.Invalidate()
		return
	}
	a.stage = 1
	a.dialStart = time.Now()
	a.progress = 0
	a.status = "正在初始化调制解调器…"
	d.Invalidate()
}

func (a *onlineServiceApp) Animate(d *Desktop, w *Window, now time.Time) {
	if a.stage != 1 {
		return
	}
	p := now.Sub(a.dialStart).Seconds() / 2.6
	if p >= 1 {
		a.stage = 2
		a.progress = 1
		a.status = "已连接（离线演示，不会产生电话费）。"
	} else {
		a.progress = p
	}
	d.Invalidate()
}

func (a *onlineServiceApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" {
		d.CloseWindow(w)
		return true
	}
	if a.stage == 1 {
		if ev.Name == "Enter" {
			a.stage = 0
			a.status = "已取消拨号。"
		}
		return false
	}
	if a.stage == 2 {
		return false
	}
	switch ev.Name {
	case "Tab":
		a.focus = (a.focus + 1) % 2
		return true
	case "Enter":
		a.connect(d)
		return true
	case "Backspace":
		if a.focus == 0 && len(a.user) > 0 {
			a.user = a.user[:len(a.user)-1]
		}
		if a.focus == 1 && len(a.pass) > 0 {
			a.pass = a.pass[:len(a.pass)-1]
		}
		d.Invalidate()
		return true
	}
	if ev.Ctrl || ev.Alt || ev.Rune < 32 {
		return false
	}
	if a.focus == 0 {
		a.user = append(a.user, ev.Rune)
	} else {
		a.pass = append(a.pass, ev.Rune)
	}
	d.Invalidate()
	return true
}

func (a *onlineServiceApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		if a.stage == 0 {
			switch {
			case a.userR.Contains(ev.X, ev.Y):
				a.focus = 0
				d.Invalidate()
			case a.passR.Contains(ev.X, ev.Y):
				a.focus = 1
				d.Invalidate()
			case a.btnConnect.Contains(ev.X, ev.Y):
				a.press = "connect"
			case a.btnCancel.Contains(ev.X, ev.Y):
				a.press = "cancel"
			}
		} else if a.stage == 1 {
			if a.btnCancel.Contains(ev.X, ev.Y) {
				a.stage = 0
				a.status = "已取消拨号。"
				d.Invalidate()
			}
		} else {
			for i, r := range a.rows {
				if r.Contains(ev.X, ev.Y) {
					a.sel = i
					d.Invalidate()
					return
				}
			}
			switch {
			case a.btnMail.Contains(ev.X, ev.Y):
				d.openInbox()
			case a.btnGames.Contains(ev.X, ev.Y):
				d.openGames()
			case a.btnBrowser.Contains(ev.X, ev.Y):
				d.openBrowser("about:home")
			case a.btnHangup.Contains(ev.X, ev.Y):
				a.stage = 0
				a.status = "已断开连接。"
				d.Invalidate()
			}
		}
	case MouseUp:
		if a.press == "connect" && a.btnConnect.Contains(ev.X, ev.Y) {
			a.press = ""
			a.connect(d)
		} else if a.press == "cancel" && a.btnCancel.Contains(ev.X, ev.Y) {
			a.press = ""
			a.stage = 0
			a.status = "已取消拨号。"
			d.Invalidate()
		}
	}
}

func (a *onlineServiceApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	banner := ui.Rect{r.X + 4, r.Y + 4, r.W - 8, 64}
	c.FillRect(banner, a.accent())
	f.TextBold(c, banner.X+16, banner.Y+10, a.brand(), ui.ColWhite)
	sub := "免费试用 · 56.6 Kbps 调制解调器"
	if a.mode == 3 {
		sub = "Microsoft Network · 56.6 Kbps 调制解调器"
	}
	f.Text(c, banner.X+16, banner.Y+34, sub, 0xFFE080)
	if a.mode == 0 {
		badge := ui.Rect{banner.Right() - 108, banner.Y + 18, 96, 26}
		c.FillRect(badge, 0xFFFF00)
		f.TextBold(c, badge.X+8, badge.Y+5, "50 小时免费", 0x000080)
	}

	switch a.stage {
	case 0:
		formX := r.X + 40
		y := banner.Bottom() + 26
		f.Text(c, formX, y, "用户名:", ui.ColText)
		a.userR = ui.Rect{formX + 70, y - 3, 280, 22}
		c.FillRect(a.userR, 0xFFFFFF)
		c.Sunken(a.userR)
		f.TextClip(c, a.userR.X+4, a.userR.Y+3, a.userR.W-8, string(a.user), ui.ColText)

		y += 34
		f.Text(c, formX, y, "密码:", ui.ColText)
		a.passR = ui.Rect{formX + 70, y - 3, 280, 22}
		c.FillRect(a.passR, 0xFFFFFF)
		c.Sunken(a.passR)
		f.TextClip(c, a.passR.X+4, a.passR.Y+3, a.passR.W-8, strings.Repeat("*", len(a.pass)), ui.ColText)

		if (a.focus == 0 || a.focus == 1) && (time.Now().UnixMilli()/500)%2 == 0 {
			field := a.userR
			value := a.user
			if a.focus == 1 {
				field = a.passR
				value = a.pass
			}
			cx := field.X + 4 + f.Width(string(value))
			if cx > field.Right()-2 {
				cx = field.Right() - 2
			}
			c.VLine(cx, field.Y+3, 16, ui.ColText)
		}

		y += 40
		a.btnConnect = ui.Rect{formX + 70, y, 100, 26}
		a.btnCancel = ui.Rect{formX + 180, y, 100, 26}
		ui.Button(c, a.btnConnect, "连接", a.press == "connect", true)
		ui.Button(c, a.btnCancel, "取消", a.press == "cancel", true)

		f.TextClip(c, formX, y+44, r.W-80, a.status, ui.ColText)
		f.TextClip(c, formX, r.Bottom()-42, r.W-80, "离线演示：不会真的拨打调制解调器，也不会联网计费。", ui.ColTextDis)
		f.TextClip(c, formX, r.Bottom()-24, r.W-80, "提示：Tab 切换输入框，Enter 连接，Esc 关闭窗口。", ui.ColTextDis)

	case 1:
		cx := r.X + r.W/2
		y := banner.Bottom() + 40
		title := "正在连接 " + a.brand() + "…"
		f.TextBold(c, cx-f.Width(title)/2, y, title, ui.ColText)
		y += 34
		bar := ui.Rect{cx - 150, y, 300, 22}
		c.FillRect(bar, 0xFFFFFF)
		c.Sunken(bar)
		fill := int(float64(bar.W-4) * a.progress)
		for yy := bar.Y + 2; yy < bar.Bottom()-2; yy++ {
			for xx := 0; xx < fill; xx++ {
				c.Set(bar.X+2+xx, yy, 0x00008B)
			}
		}
		f.TextRight(c, bar.Right(), bar.Bottom()+8, fmt.Sprintf("%d%%", int(a.progress*100)), ui.ColText)

		steps := []string{"正在初始化调制解调器…", "正在拨号 " + a.phone() + "…", "正在验证用户名…", "正在连接网络…"}
		idx := int(a.progress * float64(len(steps)))
		if idx > len(steps)-1 {
			idx = len(steps) - 1
		}
		y = bar.Bottom() + 30
		for i, s := range steps {
			col := ui.ColTextDis
			if i < idx {
				s = "√ " + s
				col = ui.ColGreen
			} else if i == idx {
				s = "→ " + s
				col = ui.ColText
			}
			f.Text(c, bar.X, y+i*20, s, col)
		}
		a.btnCancel = ui.Rect{cx - 50, r.Bottom() - 50, 100, 26}
		ui.Button(c, a.btnCancel, "取消", false, true)

	case 2:
		y := banner.Bottom() + 16
		f.TextClip(c, r.X+16, y, r.W-32, "欢迎，"+string(a.user)+"！您已连接到 "+a.brand()+"（离线演示）。", ui.ColText)
		y += 24
		listR := ui.Rect{r.X + 16, y, 180, r.Bottom() - y - 96}
		c.FillRect(listR, 0xFFFFFF)
		c.Sunken(listR)
		a.rows = a.rows[:0]
		ch := a.channels()
		for i, name := range ch {
			row := ui.Rect{listR.X + 2, listR.Y + 3 + i*24, listR.W - 4, 22}
			a.rows = append(a.rows, row)
			if i == a.sel {
				c.FillRect(row, ui.ColNavy)
				f.Text(c, row.X+8, row.Y+3, name, ui.ColWhite)
			} else {
				f.Text(c, row.X+8, row.Y+3, name, ui.ColText)
			}
		}
		panel := ui.Rect{listR.Right() + 10, y, r.Right() - 16 - (listR.Right() + 10), listR.H}
		c.FillRect(panel, 0xFFFFFF)
		c.Sunken(panel)
		if a.sel < 0 || a.sel >= len(ch) {
			a.sel = 0
		}
		lines := a.channelText(a.sel)
		for i, line := range lines {
			if panel.Y+8+i*17 > panel.Bottom()-6 {
				break
			}
			col := ui.ColText
			if i == 0 {
				col = ui.ColNavy
			}
			f.TextClip(c, panel.X+8, panel.Y+8+i*17, panel.W-16, line, col)
		}

		by := r.Bottom() - 64
		bw := 100
		gap := 8
		total := bw*4 + gap*3
		bx := r.X + (r.W-total)/2
		a.btnMail = ui.Rect{bx, by, bw, 26}
		a.btnGames = ui.Rect{bx + bw + gap, by, bw, 26}
		a.btnBrowser = ui.Rect{bx + 2*(bw+gap), by, bw, 26}
		a.btnHangup = ui.Rect{bx + 3*(bw+gap), by, bw, 26}
		ui.Button(c, a.btnMail, "打开邮件", false, true)
		ui.Button(c, a.btnGames, "打开游戏", false, true)
		ui.Button(c, a.btnBrowser, "浏览器", false, true)
		ui.Button(c, a.btnHangup, "断开连接", false, true)
		f.TextClip(c, r.X+16, r.Bottom()-32, r.W-32, a.status, ui.ColText)
	}
}

type inboxMsg struct {
	from, subject, body, date string
	read, sent, deleted       bool
}

type inboxApp struct {
	Base
	d   *Desktop
	win *Window

	msgs   []inboxMsg
	folder int
	sel    int
	status string
	press  string

	folderRects                 []ui.Rect
	msgRects                    []ui.Rect
	btnNew, btnReply, btnDelete ui.Rect
}

func (d *Desktop) openInbox() {
	for _, w := range d.windows {
		if _, ok := w.App.(*inboxApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &inboxApp{d: d, status: "单击邮件在右侧预览；未读邮件会自动标记为已读。", msgs: []inboxMsg{
		{from: "Microsoft Windows 95 团队", subject: "欢迎使用 Windows 95 Linux 26.5", date: "26-11-22 09:00",
			body: "欢迎！\n\n这台虚拟机运行完整的 Win95 风格桌面：\n极速搜索、聚光灯、系统监视器、局域网快传、\nVNC 服务器、DOOM 95 光线投射迷宫都已就绪。\n\n提示：按 Ctrl+Space 打开聚光灯搜索。"},
		{from: "Netscape Communications", subject: "Netscape Navigator 4.8 已准备就绪", date: "26-11-22 08:30",
			body: "感谢您选择 Netscape Navigator 4.8！\n\n双击桌面的 Netscape Navigator 图标即可开始安装。\n本版本包含 Navigator 浏览器、邮件与新闻组客户端。\n\n—— Netscape Communications Corporation, 1997"},
		{from: "America Online", subject: "您的 50 小时免费试用已开始", date: "26-11-22 08:10",
			body: "欢迎加入 America Online！\n\n您的免费试用已激活，包含 50 小时上网时间。\n请从开始菜单选择 AOL & Internet 免费试用。\n\n（本邮件由离线演示生成。）"},
		{from: "系统管理员", subject: "关于此虚拟机", date: "26-11-21 22:45",
			body: "本机为 ARM64 UEFI 虚拟机，所有 Win95 组件\n均由纯 Go 实现。图形控制台没有中文字体，\n因此引导与回退界面只使用英文；桌面内均为中文。\n\n如需完整 Linux 桌面，请切换到 XFCE。"},
		{from: "我", subject: "已发送：26.5 特性清单", date: "26-11-21 21:30", sent: true, read: true,
			body: "已发送邮件：\n\n26.5 特性：Everything 搜索、聚光灯、极客监视器、\n局域网快传、VNC、Z: 宿主机映射、按键精灵、DOOM 95。\n\n（示例邮件）"},
	}}
	a.win = d.Open(a)
}

func (a *inboxApp) Title() string           { return "收件箱 - Outlook Express" }
func (a *inboxApp) Icon() ui.IconKind       { return ui.IcoMail }
func (a *inboxApp) DefaultSize() (int, int) { return 760, 520 }
func (a *inboxApp) MinSize() (int, int)     { return 620, 420 }
func (a *inboxApp) Resizable() bool         { return true }

func (a *inboxApp) folderName(i int) string {
	switch i {
	case 0:
		return "收件箱"
	case 1:
		return "已发送"
	default:
		return "已删除"
	}
}

func (a *inboxApp) visible() []int {
	var out []int
	for i := range a.msgs {
		m := &a.msgs[i]
		switch a.folder {
		case 0:
			if !m.sent && !m.deleted {
				out = append(out, i)
			}
		case 1:
			if m.sent && !m.deleted {
				out = append(out, i)
			}
		default:
			if m.deleted {
				out = append(out, i)
			}
		}
	}
	return out
}

func (a *inboxApp) selected() int {
	vis := a.visible()
	if a.sel < 0 || a.sel >= len(vis) {
		return -1
	}
	return vis[a.sel]
}

func (a *inboxApp) counts(folder int) int {
	n := 0
	for i := range a.msgs {
		m := &a.msgs[i]
		switch folder {
		case 0:
			if !m.sent && !m.deleted && !m.read {
				n++
			}
		case 1:
			if m.sent && !m.deleted {
				n++
			}
		default:
			if m.deleted {
				n++
			}
		}
	}
	return n
}

func (a *inboxApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	vis := a.visible()
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
		}
		return true
	case "Down":
		if a.sel < len(vis)-1 {
			a.sel++
		}
		return true
	case "Delete":
		a.remove(d)
		return true
	case "Enter":
		if i := a.selected(); i >= 0 {
			a.msgs[i].read = true
			a.status = "正在阅读：" + a.msgs[i].subject
		}
		return true
	}
	return false
}

func (a *inboxApp) remove(d *Desktop) {
	i := a.selected()
	if i < 0 {
		return
	}
	if a.msgs[i].deleted {
		subject := a.msgs[i].subject
		a.msgs = append(a.msgs[:i], a.msgs[i+1:]...)
		a.status = "已永久删除：" + subject
	} else {
		a.msgs[i].deleted = true
		a.status = "已移到“已删除”文件夹。"
	}
	if vis := a.visible(); a.sel >= len(vis) {
		a.sel = len(vis) - 1
	}
	d.Invalidate()
}

func (a *inboxApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		for i, r := range a.folderRects {
			if r.Contains(ev.X, ev.Y) {
				a.folder = i
				a.sel = 0
				d.Invalidate()
				return
			}
		}
		for i, r := range a.msgRects {
			if r.Contains(ev.X, ev.Y) {
				a.sel = i
				if idx := a.selected(); idx >= 0 {
					a.msgs[idx].read = true
				}
				d.Invalidate()
				return
			}
		}
		switch {
		case a.btnNew.Contains(ev.X, ev.Y):
			d.openNotepad("")
		case a.btnReply.Contains(ev.X, ev.Y):
			if i := a.selected(); i >= 0 {
				a.status = "回复 " + a.msgs[i].from + "：请使用“新邮件”在记事本中撰写。"
			}
		case a.btnDelete.Contains(ev.X, ev.Y):
			a.remove(d)
		}
	}
}

func (a *inboxApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	by := r.Y + 6
	a.btnNew = ui.Rect{r.X + 8, by, 84, 26}
	a.btnReply = ui.Rect{r.X + 96, by, 70, 26}
	a.btnDelete = ui.Rect{r.X + 170, by, 70, 26}
	ui.Button(c, a.btnNew, "新邮件", false, true)
	ui.Button(c, a.btnReply, "回复", false, true)
	ui.Button(c, a.btnDelete, "删除", false, true)

	top := by + 34
	bottom := r.Bottom() - 26

	folders := ui.Rect{r.X + 8, top, 140, bottom - top}
	c.FillRect(folders, 0xFFFFFF)
	c.Sunken(folders)
	a.folderRects = a.folderRects[:0]
	for i := 0; i < 3; i++ {
		row := ui.Rect{folders.X + 2, folders.Y + 3 + i*24, folders.W - 4, 22}
		a.folderRects = append(a.folderRects, row)
		label := a.folderName(i)
		if n := a.counts(i); n > 0 {
			label += fmt.Sprintf(" (%d)", n)
		}
		if i == a.folder {
			c.FillRect(row, ui.ColNavy)
			f.Text(c, row.X+8, row.Y+3, label, ui.ColWhite)
		} else {
			f.Text(c, row.X+8, row.Y+3, label, ui.ColText)
		}
	}

	list := ui.Rect{folders.Right() + 8, top, 250, bottom - top}
	c.FillRect(list, 0xFFFFFF)
	c.Sunken(list)
	vis := a.visible()
	a.msgRects = a.msgRects[:0]
	yy := list.Y + 3
	for i, idx := range vis {
		row := ui.Rect{list.X + 2, yy, list.W - 4, 34}
		if yy+34 > list.Bottom()-2 {
			break
		}
		a.msgRects = append(a.msgRects, row)
		if i == a.sel {
			c.FillRect(row, ui.ColNavy)
		}
		m := &a.msgs[idx]
		tcol, scol := ui.ColText, ui.ColTextDis
		if i == a.sel {
			tcol, scol = ui.ColWhite, ui.ColLight
		}
		mark := ""
		if !m.read {
			mark = "● "
		}
		if i == a.sel {
			f.TextClip(c, row.X+6, row.Y+3, row.W-12, mark+m.subject, tcol)
		} else if !m.read {
			f.TextBold(c, row.X+6, row.Y+3, mark+m.subject, tcol)
		} else {
			f.TextClip(c, row.X+6, row.Y+3, row.W-12, mark+m.subject, tcol)
		}
		f.TextClip(c, row.X+6, row.Y+19, row.W-12, m.from+"  ·  "+m.date, scol)
		yy += 35
	}
	if len(vis) == 0 {
		f.Text(c, list.X+8, list.Y+8, "（此文件夹为空）", ui.ColTextDis)
	}

	preview := ui.Rect{list.Right() + 8, top, r.Right() - 8 - (list.Right() + 8), bottom - top}
	c.FillRect(preview, 0xFFFFFF)
	c.Sunken(preview)
	if idx := a.selected(); idx >= 0 {
		m := &a.msgs[idx]
		y := preview.Y + 8
		f.TextBold(c, preview.X+10, y, m.subject, ui.ColNavy)
		y += 22
		f.TextClip(c, preview.X+10, y, preview.W-20, "发件人："+m.from, ui.ColText)
		y += 18
		f.TextClip(c, preview.X+10, y, preview.W-20, "日期："+m.date, ui.ColTextDis)
		y += 24
		c.HLine(preview.X+10, y, preview.W-20, ui.ColShadow)
		c.HLine(preview.X+10, y+1, preview.W-20, ui.ColWhite)
		y += 10
		for _, line := range strings.Split(m.body, "\n") {
			if y+16 > preview.Bottom()-8 {
				break
			}
			f.TextClip(c, preview.X+10, y, preview.W-20, line, ui.ColText)
			y += 17
		}
	} else {
		f.Text(c, preview.X+10, preview.Y+10, "选择一封邮件以查看内容。", ui.ColTextDis)
	}

	f.TextClip(c, r.X+8, r.Bottom()-20, r.W-16, a.status, ui.ColText)
}

var briefcaseDirPath = "/root/公文包"
var briefcaseOriginFile = "/root/公文包/.origins.json"

type briefEntry struct {
	name string
	path string
	size int64
	mod  time.Time
}

type briefcaseApp struct {
	Base
	d   *Desktop
	win *Window

	entries []briefEntry
	sel     int
	status  string
	press   string

	rows                                 []ui.Rect
	btnAdd, btnSync, btnOpen, btnRefresh ui.Rect
}

func (d *Desktop) openBriefcase() {
	for _, w := range d.windows {
		if app, ok := w.App.(*briefcaseApp); ok {
			app.load()
			d.bringToFront(w)
			return
		}
	}
	a := &briefcaseApp{d: d}
	a.load()
	a.win = d.Open(a)
}

func (a *briefcaseApp) Title() string           { return "我的公文包" }
func (a *briefcaseApp) Icon() ui.IconKind       { return ui.IcoBriefcase }
func (a *briefcaseApp) DefaultSize() (int, int) { return 620, 420 }
func (a *briefcaseApp) MinSize() (int, int)     { return 500, 340 }
func (a *briefcaseApp) Resizable() bool         { return true }

func (a *briefcaseApp) load() {
	a.entries = a.entries[:0]
	ents, err := os.ReadDir(briefcaseDirPath)
	if err == nil {
		for _, e := range ents {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			a.entries = append(a.entries, briefEntry{name: e.Name(), path: filepath.Join(briefcaseDirPath, e.Name()), size: info.Size(), mod: info.ModTime()})
		}
	}
	sort.Slice(a.entries, func(i, j int) bool { return a.entries[i].name < a.entries[j].name })
	if a.sel >= len(a.entries) {
		a.sel = len(a.entries) - 1
	}
	if a.status == "" {
		a.status = "把文件放进公文包，离开后“同步”即可把较新的版本更新回原位置。"
	}
}

func briefcaseOrigins() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(briefcaseOriginFile)
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func saveBriefcaseOrigins(m map[string]string) {
	_ = os.MkdirAll(briefcaseDirPath, 0o755)
	data, err := json.MarshalIndent(m, "", "  ")
	if err == nil {
		_ = os.WriteFile(briefcaseOriginFile, data, 0o644)
	}
}

func uniqueBriefName(name string) string {
	base := name
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(briefcaseDirPath, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s (%d)%s", stem, i, ext)
	}
	return base
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (a *briefcaseApp) add(d *Desktop) {
	d.FileDialog(FileOpen, "/root/桌面", "", func(path string, ok bool) {
		if !ok || path == "" {
			return
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			d.MessageBox("我的公文包", "请选择要放入公文包的文件。", IconInfo, []string{"确定"}, nil)
			return
		}
		if err := os.MkdirAll(briefcaseDirPath, 0o755); err != nil {
			d.MessageBox("我的公文包", "无法创建公文包目录：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		name := uniqueBriefName(filepath.Base(path))
		if err := copyFileTo(path, filepath.Join(briefcaseDirPath, name)); err != nil {
			d.MessageBox("我的公文包", "无法复制文件：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		origins := briefcaseOrigins()
		origins[name] = path
		saveBriefcaseOrigins(origins)
		a.load()
		a.status = "已放入公文包：" + name
		d.Invalidate()
	})
}

func (a *briefcaseApp) sync(d *Desktop) {
	origins := briefcaseOrigins()
	updated, missing := 0, 0
	for _, e := range a.entries {
		origin := origins[e.name]
		if origin == "" {
			continue
		}
		ost, err := os.Stat(origin)
		if err != nil {
			missing++
			continue
		}
		if !e.mod.After(ost.ModTime()) && e.size == ost.Size() {
			continue
		}
		if err := copyFileTo(e.path, origin); err != nil {
			d.MessageBox("我的公文包", "同步失败："+e.name+"\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		updated++
	}
	a.status = fmt.Sprintf("同步完成：更新了 %d 个文件。", updated)
	if missing > 0 {
		a.status += fmt.Sprintf(" %d 个原文件已不存在。", missing)
	}
	a.load()
	d.Invalidate()
}

func (a *briefcaseApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		for i, r := range a.rows {
			if r.Contains(ev.X, ev.Y) {
				if i == a.sel {
					d.openFileDefault(a.entries[i].path)
				} else {
					a.sel = i
					d.Invalidate()
				}
				return
			}
		}
		a.press = ""
		switch {
		case a.btnAdd.Contains(ev.X, ev.Y):
			a.add(d)
		case a.btnSync.Contains(ev.X, ev.Y):
			a.sync(d)
		case a.btnOpen.Contains(ev.X, ev.Y):
			if a.sel >= 0 && a.sel < len(a.entries) {
				d.openFileDefault(a.entries[a.sel].path)
			}
		case a.btnRefresh.Contains(ev.X, ev.Y):
			a.load()
			d.Invalidate()
		}
	}
}

func (a *briefcaseApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	by := r.Y + 6
	a.btnAdd = ui.Rect{r.X + 8, by, 96, 26}
	a.btnSync = ui.Rect{r.X + 108, by, 96, 26}
	a.btnOpen = ui.Rect{r.X + 208, by, 70, 26}
	a.btnRefresh = ui.Rect{r.X + 282, by, 70, 26}
	ui.Button(c, a.btnAdd, "添加文件…", false, true)
	ui.Button(c, a.btnSync, "同步", false, true)
	ui.Button(c, a.btnOpen, "打开", false, true)
	ui.Button(c, a.btnRefresh, "刷新", false, true)

	list := ui.Rect{r.X + 8, by + 34, r.W - 16, r.Bottom() - (by + 34) - 28}
	c.FillRect(list, 0xFFFFFF)
	c.Sunken(list)
	f.Text(c, list.X+6, list.Y+4, "名称", ui.ColTextDis)
	f.TextRight(c, list.Right()-96, list.Y+4, "大小", ui.ColTextDis)
	f.TextRight(c, list.Right()-8, list.Y+4, "修改时间", ui.ColTextDis)
	c.HLine(list.X+2, list.Y+20, list.W-4, ui.ColShadow)
	a.rows = a.rows[:0]
	yy := list.Y + 22
	for i, e := range a.entries {
		if yy+20 > list.Bottom()-2 {
			break
		}
		row := ui.Rect{list.X + 2, yy, list.W - 4, 19}
		a.rows = append(a.rows, row)
		if i == a.sel {
			c.FillRect(row, ui.ColNavy)
		}
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		ui.DrawIcon16(c, ui.IcoFile, row.X+3, row.Y+1)
		f.TextClip(c, row.X+24, row.Y+2, row.W-140, e.name, col)
		f.TextRight(c, row.Right()-96, row.Y+2, sizeText(e.size), col)
		f.TextRight(c, row.Right()-8, row.Y+2, e.mod.Format("01-02 15:04"), col)
		yy += 20
	}
	if len(a.entries) == 0 {
		f.Text(c, list.X+8, list.Y+26, "（公文包是空的，点击“添加文件…”）", ui.ColTextDis)
	}
	f.TextClip(c, r.X+8, r.Bottom()-20, r.W-16, a.status, ui.ColText)
}

type quickLaunch struct {
	name string
	desc string
	icon ui.IconKind
	open func(*Desktop)
}

type launcherApp struct {
	Base
	d      *Desktop
	win    *Window
	kind   int
	sel    int
	rows   []ui.Rect
	status string
}

func (d *Desktop) openGames() {
	for _, w := range d.windows {
		if app, ok := w.App.(*launcherApp); ok && app.kind == 0 {
			d.bringToFront(w)
			return
		}
	}
	a := &launcherApp{d: d, kind: 0, status: "双击游戏即可启动。"}
	a.win = d.Open(a)
}

func (d *Desktop) openOnlineServices() {
	for _, w := range d.windows {
		if app, ok := w.App.(*launcherApp); ok && app.kind == 1 {
			d.bringToFront(w)
			return
		}
	}
	a := &launcherApp{d: d, kind: 1, status: "双击服务即可连接；全部为离线演示。"}
	a.win = d.Open(a)
}

func (a *launcherApp) Title() string {
	if a.kind == 0 {
		return "游戏"
	}
	return "在线服务"
}

func (a *launcherApp) Icon() ui.IconKind {
	if a.kind == 0 {
		return ui.IcoGame
	}
	return ui.IcoOnline
}

func (a *launcherApp) DefaultSize() (int, int) { return 540, 420 }
func (a *launcherApp) MinSize() (int, int)     { return 460, 320 }
func (a *launcherApp) Resizable() bool         { return true }

func (a *launcherApp) items() []quickLaunch {
	if a.kind == 0 {
		return []quickLaunch{
			{"纸牌", "经典 Windows 纸牌游戏", ui.IcoCards, func(d *Desktop) { d.openSolitaire() }},
			{"扫雷", "找出所有地雷", ui.IcoMine, func(d *Desktop) { d.openMines() }},
			{"三维弹球", "Space Cadet 弹珠台", ui.IcoGame, func(d *Desktop) { d.openPinball() }},
			{"方块世界 95", "体素沙盒与 3D 渲染", ui.IcoGame, func(d *Desktop) { d.openBlockWorld() }},
			{"DOOM 95", "纯 Go 光线投射迷宫", ui.IcoMaze, func(d *Desktop) { d.openDoom() }},
		}
	}
	return []quickLaunch{
		{"AOL & Internet 免费试用", "50 小时免费试用（离线演示）", ui.IcoOnline, func(d *Desktop) { d.openAOL(0) }},
		{"AOL 3.0i", "含 Internet Explorer 4.0", ui.IcoOnline, func(d *Desktop) { d.openAOL(1) }},
		{"AOL Canada 3.0i", "加拿大版，含 IE 4.0", ui.IcoOnline, func(d *Desktop) { d.openAOL(2) }},
		{"The Microsoft Network", "MSN 拨号与今日内容", ui.IcoMSN, func(d *Desktop) { d.openMSN() }},
		{"Internet Explorer", "打开内置浏览器", ui.IcoIE, func(d *Desktop) { d.openBrowser("about:home") }},
		{"Netscape Navigator", "安装并启动 Netscape 4.8", ui.IcoNetscape, func(d *Desktop) { d.openNetscape() }},
	}
}

func (a *launcherApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	items := a.items()
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Up":
		if a.sel > 0 {
			a.sel--
		}
		return true
	case "Down":
		if a.sel < len(items)-1 {
			a.sel++
		}
		return true
	case "Enter":
		if a.sel >= 0 && a.sel < len(items) {
			items[a.sel].open(d)
		}
		return true
	}
	return false
}

func (a *launcherApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 || ev.Kind != MouseDown {
		return
	}
	items := a.items()
	for i, r := range a.rows {
		if !r.Contains(ev.X, ev.Y) {
			continue
		}
		if i == a.sel && i < len(items) {
			items[i].open(d)
		} else {
			a.sel = i
			d.Invalidate()
		}
		return
	}
}

func (a *launcherApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.TextClip(c, r.X+12, r.Y+8, r.W-24, a.status, ui.ColText)
	list := ui.Rect{r.X + 8, r.Y + 30, r.W - 16, r.Bottom() - 38}
	c.FillRect(list, 0xFFFFFF)
	c.Sunken(list)
	a.rows = a.rows[:0]
	items := a.items()
	yy := list.Y + 3
	for i, it := range items {
		row := ui.Rect{list.X + 2, yy, list.W - 4, 46}
		if yy+46 > list.Bottom()-2 {
			break
		}
		a.rows = append(a.rows, row)
		if i == a.sel {
			c.FillRect(row, ui.ColNavy)
		}
		ui.DrawIcon32(c, it.icon, row.X+8, row.Y+7, false)
		col := ui.ColText
		if i == a.sel {
			col = ui.ColWhite
		}
		f.TextBold(c, row.X+50, row.Y+6, it.name, col)
		f.TextClip(c, row.X+50, row.Y+25, row.W-60, it.desc, ui.ColTextDis)
		yy += 47
	}
}
