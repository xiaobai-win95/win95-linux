package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

var wineBinary = "wine"
var winePrefixDir = "/root/.wine"

func wineAvailable() bool {
	_, err := exec.LookPath(wineBinary)
	return err == nil
}

func wineVersion() string {
	if !wineAvailable() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, wineBinary, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func wineCDrive() string { return filepath.Join(winePrefixDir, "drive_c") }

type WineApp struct {
	Base
	d   *Desktop
	win *Window

	status   string
	log      string
	busy     bool
	lastTick time.Time

	btnInstall, btnRun, btnConfig, btnCDrive, btnRefresh ui.Rect
}

func (d *Desktop) openWine() *WineApp {
	for _, w := range d.windows {
		if a, ok := w.App.(*WineApp); ok {
			a.refresh()
			d.bringToFront(w)
			return a
		}
	}
	a := &WineApp{d: d}
	a.refresh()
	a.win = d.Open(a)
	return a
}

func (a *WineApp) refresh() {
	if ver := wineVersion(); ver != "" {
		a.status = "Wine 已安装：" + ver + " · 前缀 " + winePrefixDir
	} else {
		a.status = "尚未安装 Wine。运行 Windows 程序前必须先安装兼容层。"
	}
}

func (a *WineApp) Title() string           { return "Wine 兼容层 - Windows 程序" }
func (a *WineApp) Icon() ui.IconKind       { return ui.IcoWine }
func (a *WineApp) DefaultSize() (int, int) { return 660, 460 }
func (a *WineApp) MinSize() (int, int)     { return 560, 400 }
func (a *WineApp) Resizable() bool         { return true }

func (a *WineApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "运行 Windows 程序…", Enabled: true, Action: func() { a.runDialog(d) }},
			{Label: "打开 C: 盘", Enabled: true, Action: func() { a.openCDrive(d) }},
			{Separator: true},
			{Label: "关闭(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "工具(T)", Items: []MenuItem{
			{Label: "Wine 配置(winecfg)", Enabled: true, Action: func() { a.openConfig() }},
			{Label: "安装 Wine", Enabled: true, Action: func() { a.install(d) }},
			{Label: "刷新状态(F5)", Enabled: true, Action: func() { a.refresh(); d.Invalidate() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于 Wine 兼容层", Enabled: true, Action: func() {
				d.MessageBox("关于 Wine 兼容层",
					"Wine 11（Alpine community/aarch64）\n\n"+
						"Wine 让 Windows PE 程序直接在 Linux 上运行，无需虚拟机。\n"+
						"本机为 ARM64：可运行 ARM64 Windows 程序；x86 程序还需要\n"+
						"Box64/Box86 或 Arch Linux 子系统里的对应版本。\n\n"+
						"Wine 前缀：/root/.wine\nWine 配置会出现在 X11 嵌套窗口中。",
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *WineApp) install(d *Desktop) {
	if a.busy {
		return
	}
	if wineAvailable() {
		a.refresh()
		d.MessageBox("Wine 兼容层", "Wine 已经安装。\n\n"+a.status, IconInfo, []string{"确定"}, nil)
		return
	}
	d.MessageBox("安装 Wine",
		"将从 Alpine 软件仓库安装 wine（含依赖，约 200–400 MB）。\n\n"+
			"安装完成后即可运行 Windows 程序。是否继续？",
		IconQuestion, []string{"安装", "取消"}, func(i int) {
			if i == 0 {
				a.startInstall(d)
			}
		})
}

func (a *WineApp) startInstall(d *Desktop) {
	a.busy = true
	a.status = "正在安装 Wine（apk add wine）…"
	a.log = "$ apk add --no-cache wine\n"
	d.Invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/sbin/apk", "add", "--no-cache", "wine")
		cmd.Env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"LANG=C.UTF-8",
		}
		out, err := cmd.CombinedOutput()
		text := string(out)
		if len(text) > 32<<10 {
			text = text[:32<<10] + "\n…（输出已截断）"
		}
		d.Post(func() {
			a.busy = false
			a.log += text
			if err != nil {
				a.status = "Wine 安装失败：" + err.Error()
			} else if wineAvailable() {
				a.refresh()
			} else {
				a.status = "apk 报告安装完成，但仍找不到 wine 命令。"
			}
			d.Invalidate()
		})
	}()
}

func (a *WineApp) runDialog(d *Desktop) {
	if !wineAvailable() {
		d.MessageBox("Wine 兼容层", "运行 Windows 程序前必须先安装 Wine。\n\n请先在 Wine 兼容层窗口中点击“安装 Wine”。",
			IconWarning, []string{"确定"}, nil)
		return
	}
	d.FileDialog(FileOpen, "/root", "", func(path string, ok bool) {
		if ok {
			d.openWineProgram(path)
		}
	})
}

func (a *WineApp) openCDrive(d *Desktop) {
	if err := os.MkdirAll(wineCDrive(), 0o755); err != nil {
		d.MessageBox("Wine 兼容层", "无法创建 C: 盘目录：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	d.openFiles(wineCDrive())
}

func (a *WineApp) openConfig() {
	if !wineAvailable() {
		a.d.MessageBox("Wine 兼容层", "winecfg 需要先安装 Wine。", IconWarning, []string{"确定"}, nil)
		return
	}
	px := a.d.x11ProxyWindow("", nil)
	if px != nil {
		px.launchArgv([]string{"winecfg"})
	}
}

func (a *WineApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "F5":
		a.refresh()
		d.Invalidate()
		return true
	}
	return false
}

func (a *WineApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	switch {
	case a.btnInstall.Contains(ev.X, ev.Y):
		a.install(d)
	case a.btnRun.Contains(ev.X, ev.Y):
		a.runDialog(d)
	case a.btnConfig.Contains(ev.X, ev.Y):
		a.openConfig()
	case a.btnCDrive.Contains(ev.X, ev.Y):
		a.openCDrive(d)
	case a.btnRefresh.Contains(ev.X, ev.Y):
		a.refresh()
		d.Invalidate()
	}
}

func (a *WineApp) Tick(d *Desktop, w *Window, now time.Time) {
	if now.Sub(a.lastTick) < 3*time.Second {
		return
	}
	a.lastTick = now
	old := a.status
	a.refresh()
	if a.status != old {
		d.Invalidate()
	}
}

func (a *WineApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	header := ui.Rect{r.X, r.Y, r.W, 52}
	installed := wineAvailable()
	top, bottom := uint32(0x4B0000), uint32(0x8B0000)
	if installed {
		top, bottom = 0x1F3864, 0x2F5496
	}
	c.TitleBar(header, top, bottom)
	ui.DrawIcon32(c, ui.IcoWine, header.X+12, header.Y+10, false)
	f.TextBold(c, header.X+56, header.Y+8, "Wine 兼容层", ui.ColWhite)
	ver := "未安装"
	if installed {
		ver = wineVersion()
		if ver == "" {
			ver = "已安装"
		}
	}
	f.Text(c, header.X+56, header.Y+28, ver+" · ARM64 / Alpine 3.24", 0xC8DCFF)

	y := header.Bottom() + 12
	statusBox := ui.Rect{r.X + 12, y, r.W - 24, 44}
	c.FillRect(statusBox, 0xFFFFFF)
	c.Sunken(statusBox)
	f.TextClip(c, statusBox.X+8, statusBox.Y+6, statusBox.W-16, a.status, ui.ColText)
	if a.busy {
		f.TextClip(c, statusBox.X+8, statusBox.Y+24, statusBox.W-16, "安装可能需要几分钟，请保持网络连接。", ui.ColTextDis)
	}

	y = statusBox.Bottom() + 10
	bw, gap := 122, 8
	a.btnInstall = ui.Rect{r.X + 12, y, bw, 28}
	a.btnRun = ui.Rect{a.btnInstall.Right() + gap, y, bw, 28}
	a.btnConfig = ui.Rect{a.btnRun.Right() + gap, y, bw, 28}
	a.btnCDrive = ui.Rect{a.btnConfig.Right() + gap, y, bw, 28}
	a.btnRefresh = ui.Rect{a.btnCDrive.Right() + gap, y, 80, 28}
	ui.Button(c, a.btnInstall, "安装 Wine", a.busy, !a.busy)
	ui.Button(c, a.btnRun, "运行程序…", false, installed)
	ui.Button(c, a.btnConfig, "winecfg", false, installed)
	ui.Button(c, a.btnCDrive, "打开 C: 盘", false, true)
	ui.Button(c, a.btnRefresh, "刷新", false, true)

	y = a.btnInstall.Bottom() + 12
	logBox := ui.Rect{r.X + 12, y, r.W - 24, r.Bottom() - y - 62}
	c.FillRect(logBox, 0x101010)
	c.Sunken(logBox)
	if a.log == "" {
		f.Text(c, logBox.X+8, logBox.Y+8, "安装日志会显示在这里。", 0x9A9A9A)
	} else {
		lines := strings.Split(strings.TrimRight(a.log, "\n"), "\n")
		maxLines := (logBox.H - 10) / 16
		start := len(lines) - maxLines
		if start < 0 {
			start = 0
		}
		for i := 0; i < maxLines && start+i < len(lines); i++ {
			f.TextClip(c, logBox.X+8, logBox.Y+6+i*16, logBox.W-16, lines[start+i], 0xD0D0D0)
		}
	}
	notes := "提示：Wine 11 来自 Alpine community。C: 盘位于 " + wineCDrive() + "；ARM64 主机可运行 ARM64 Windows 程序。"
	f.TextClip(c, r.X+12, r.Bottom()-38, r.W-24, notes, ui.ColTextDis)
	f.TextClip(c, r.X+12, r.Bottom()-20, r.W-24, "x86 Windows 程序需要额外的 Box64/Box86 或 Arch 子系统。", ui.ColTextDis)
}

// openWineProgram launches a Windows executable inside the nested X11 proxy.
func (d *Desktop) openWineProgram(path string) {
	if !wineAvailable() {
		d.MessageBox("Wine 兼容层", "尚未安装 Wine，无法运行：\n"+filepath.Base(path)+"\n\n请先打开“Wine 兼容层”并安装 Wine。",
			IconWarning, []string{"确定"}, nil)
		d.openWine()
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	var argv []string
	switch ext {
	case ".msi":
		argv = []string{wineBinary, "msiexec", "/i", path}
	case ".bat", ".cmd":
		argv = []string{wineBinary, "cmd", "/c", path}
	default:
		argv = []string{wineBinary, path}
	}
	px := d.x11ProxyWindow("", nil)
	if px == nil {
		return
	}
	if px.win != nil {
		d.bringToFront(px.win)
		d.maximizeWindow(px.win)
	}
	px.launchArgv(argv)
}
