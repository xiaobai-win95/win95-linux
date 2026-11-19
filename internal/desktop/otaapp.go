package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"win95linux/internal/ota"
	"win95linux/internal/ui"
)

type otaPhase int

const (
	otaIdle otaPhase = iota
	otaChecking
	otaUpToDate
	otaAvailable
	otaDownloading
	otaApplying
	otaReady
	otaFailed
)

// OTAApp is the Windows Update style client for GitHub-hosted releases.
type OTAApp struct {
	Base
	d *Desktop

	phase    otaPhase
	current  string
	slot     string
	manifest ota.Manifest
	status   string
	notes    []string
	log      []string

	progress int // -1 for indeterminate
	cancel   context.CancelFunc

	press string

	checkR, installR         ui.Rect
	cancelR, rebootR, closeR ui.Rect
	progressR, notesR, logR  ui.Rect
}

func NewOTAApp(d *Desktop) *OTAApp {
	a := &OTAApp{d: d, progress: -1, slot: ota.NormalizeSlot(os.Getenv("WIN95_SLOT"))}
	a.current = ota.ReadVersionFile(ota.VersionFilePath).Version
	if a.current == "" {
		a.current = "未知"
	}
	a.status = "正在检查最新版本..."
	a.startCheck()
	return a
}

func (a *OTAApp) Title() string           { return "系统更新" }
func (a *OTAApp) Icon() ui.IconKind       { return ui.IcoPackage }
func (a *OTAApp) DefaultSize() (int, int) { return 600, 460 }
func (a *OTAApp) MinSize() (int, int)     { return 520, 400 }
func (a *OTAApp) Resizable() bool         { return true }

func (a *OTAApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "关闭", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "更新(U)", Items: []MenuItem{
			{Label: "检查更新", Accel: "F5", Enabled: !a.busy(), Action: func() { a.startCheck() }},
			{Label: "下载并安装更新", Enabled: a.canInstall(), Action: func() { a.startInstall() }},
			{Separator: true},
			{Label: "立即重启", Enabled: a.phase == otaReady, Action: func() { a.reboot() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于系统更新", Enabled: true, Action: func() {
				d.MessageBox("关于系统更新",
					"Windows 95 Linux 系统更新\n\n"+
						"从 GitHub Releases 获取新版本，使用共享超级分区上的 A/B 双插槽安装。\n"+
						"更新写入当前未使用的插槽，新系统首次启动处于“试用”状态；\n"+
						"连续两次无法正常进入桌面时，引导程序会自动回退到旧插槽。\n\n"+
						"更新源: GitHub 直连 + 公共加速镜像，检查时并行探测、下载时自动选最快源。\n"+
						"可在 /etc/win95-ota.conf 中自定义 MANIFEST_URL、MIRROR，或用 DIRECT=0 关闭直连。\n\n"+
						"清单地址: "+ota.LoadSources().ManifestURL,
					IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *OTAApp) Close() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *OTAApp) busy() bool {
	return a.phase == otaChecking || a.phase == otaDownloading || a.phase == otaApplying
}

func (a *OTAApp) canInstall() bool {
	return a.phase == otaAvailable && a.manifest.Version != "" && ota.IsNewerVersion(a.manifest.Version, a.current)
}

func (a *OTAApp) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.log = append(a.log, line)
	if len(a.log) > 100 {
		a.log = a.log[len(a.log)-100:]
	}
	a.d.Invalidate()
}

func (a *OTAApp) startCheck() {
	if a.busy() {
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.phase = otaChecking
	a.progress = -1
	a.status = "正在检查最新版本..."
	a.appendLog("正在并行探测更新源（GitHub 直连与镜像）")
	a.d.Invalidate()

	go func() {
		src := ota.LoadSources()
		logf := func(line string) {
			a.d.Post(func() {
				if ctx.Err() != nil {
					return
				}
				a.appendLog(line)
			})
		}
		m, _, err := ota.FetchManifestAuto(ctx, src.ManifestCandidates(), logf)
		a.d.Post(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				a.phase = otaFailed
				a.progress = -1
				a.status = "检查更新失败"
				a.appendLog("失败: " + err.Error())
				a.d.Invalidate()
				return
			}
			a.manifest = m
			a.notes = splitNotes(m.Notes)
			if ota.IsNewerVersion(m.Version, a.current) {
				a.phase = otaAvailable
				a.progress = 100
				a.status = fmt.Sprintf("发现新版本 %s（当前 %s）", m.Version, a.current)
				a.appendLog("发现新版本 " + m.Version)
				if m.Package.Size > 0 {
					a.appendLog(fmt.Sprintf("更新包大小: %s", formatBytes(m.Package.Size)))
				}
			} else {
				a.phase = otaUpToDate
				a.progress = 100
				a.status = fmt.Sprintf("已是最新版本（%s）", a.current)
				a.appendLog("当前版本已是最新")
			}
			a.d.Invalidate()
		})
	}()
}

func splitNotes(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

func (a *OTAApp) startInstall() {
	if !a.canInstall() || a.busy() {
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	m := a.manifest
	a.phase = otaDownloading
	a.progress = 0
	a.status = "正在下载更新包..."
	a.appendLog("开始下载 " + m.Version + "（自动选择最快源）")
	a.d.Invalidate()

	go func() {
		fail := func(err error) {
			a.d.Post(func() {
				if ctx.Err() != nil {
					return
				}
				a.phase = otaFailed
				a.progress = -1
				a.status = "更新失败"
				a.appendLog("失败: " + err.Error())
				a.d.Invalidate()
			})
		}
		superDir, err := ota.EnsureSuperMounted()
		if err != nil {
			fail(fmt.Errorf("超级分区不可用: %w", err))
			return
		}
		pkgPath := filepath.Join(superDir, ota.CacheDir, "win95-ota-"+m.Version+".tar.gz")
		size := m.Package.Size
		src := ota.LoadSources()
		err = ota.DownloadPackageAuto(ctx, src.PackageCandidates(m.Package.URL), pkgPath, func(done, total int64) {
			if total <= 0 {
				a.d.Post(func() { a.progress = -1 })
				return
			}
			pct := int(done * 60 / total)
			a.d.Post(func() { a.progress = pct })
		}, func(line string) {
			a.d.Post(func() {
				if ctx.Err() == nil {
					a.appendLog(line)
				}
			})
		})
		if err != nil {
			fail(err)
			return
		}
		a.d.Post(func() {
			a.progress = 62
			a.status = "正在校验更新包..."
			a.d.Invalidate()
		})
		if size > 0 {
			if st, err := os.Stat(pkgPath); err == nil && st.Size() != size {
				os.Remove(pkgPath)
				fail(fmt.Errorf("更新包大小不符（期望 %d，实际 %d）", size, st.Size()))
				return
			}
		}
		sum, err := ota.SHA256File(pkgPath)
		if err != nil {
			fail(err)
			return
		}
		if !strings.EqualFold(sum, m.Package.SHA256) {
			os.Remove(pkgPath)
			fail(fmt.Errorf("更新包 SHA-256 校验失败"))
			return
		}
		a.d.Post(func() {
			a.phase = otaApplying
			a.progress = 65
			a.status = "正在把新系统写入备用插槽..."
			a.appendLog("校验通过，开始写入插槽")
			a.d.Invalidate()
		})
		res, err := ota.ApplyUpdate(superDir, pkgPath, ota.SuperUUIDFromCmdline(), a.current,
			func(pct int, msg string) {
				composite := 65
				if pct >= 0 {
					composite = 65 + pct*35/100
				}
				a.d.Post(func() {
					if pct >= 0 {
						a.progress = composite
					}
					if msg != "" {
						a.status = msg
					}
					a.d.Invalidate()
				})
			})
		if err != nil {
			fail(err)
			return
		}
		os.Remove(pkgPath)
		a.d.Post(func() {
			a.phase = otaReady
			a.progress = 100
			a.status = "更新已写入插槽 " + strings.ToUpper(res.Slot) + "，重启后生效"
			a.appendLog("新版本 " + res.Version + " 已写入插槽 " + strings.ToUpper(res.Slot))
			a.appendLog("重启后进入新系统；若两次启动失败会自动回退")
			a.d.Invalidate()
		})
	}()
}

func (a *OTAApp) cancelRun() {
	if a.cancel != nil {
		a.cancel()
	}
	a.phase = otaIdle
	a.progress = -1
	a.status = "已取消"
	a.appendLog("操作已取消")
	a.d.Invalidate()
}

func (a *OTAApp) reboot() {
	if a.d.Host.Reboot != nil {
		a.d.Host.Reboot()
		return
	}
	a.d.MessageBox("系统更新", "当前环境无法直接重启，请手动关闭虚拟机并重新开机。", IconInfo, []string{"确定"}, nil)
}

func (a *OTAApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	x := r.X + 14
	right := r.Right() - 14
	width := right - x

	f.TextBold(c, x, r.Y+12, "Windows 更新", ui.ColText)
	slot := ""
	if a.slot != "" {
		slot = " · 插槽 " + strings.ToUpper(a.slot)
	}
	f.Text(c, x, r.Y+34, "当前版本 "+a.current+slot, ui.ColText)

	a.status = strings.TrimSpace(a.status)
	y := r.Y + 60
	for i, ln := range wrapText(a.status, width) {
		if i >= 2 {
			break
		}
		f.Text(c, x, y, ln, ui.ColText)
		y += 18
	}

	a.progressR = ui.Rect{x, y + 4, width, 20}
	if a.progress < 0 {
		// Determinate bar with a scanned stripe is enough to show activity
		// without needing a timer.
		ui.ProgressBar(c, a.progressR, 30)
	} else {
		ui.ProgressBar(c, a.progressR, a.progress)
	}
	if a.progress >= 0 {
		f.TextRight(c, right, a.progressR.Y+2, fmt.Sprintf("%d%%", a.progress), ui.ColText)
	}

	notesTop := a.progressR.Bottom() + 10
	f.Text(c, x, notesTop, "更新说明:", ui.ColText)
	a.notesR = ui.Rect{x, notesTop + 20, width, 70}
	c.FillRect(a.notesR, ui.ColWhite)
	c.Sunken(a.notesR)
	if len(a.notes) == 0 {
		f.TextClip(c, a.notesR.X+6, a.notesR.Y+5, a.notesR.W-12, "（暂无更新说明）", ui.ColTextDis)
	} else {
		yy := a.notesR.Y + 5
		for _, ln := range a.notes {
			if yy+16 > a.notesR.Bottom()-3 {
				break
			}
			f.TextClip(c, a.notesR.X+6, yy, a.notesR.W-12, ln, ui.ColText)
			yy += 16
		}
	}

	logTop := a.notesR.Bottom() + 10
	a.logR = ui.Rect{x, logTop, width, r.Bottom() - 48 - logTop}
	if a.logR.H < 30 {
		a.logR.H = 30
	}
	c.FillRect(a.logR, ui.ColWhite)
	c.Sunken(a.logR)
	lines := a.log
	if max := a.logR.H / 16; len(lines) > max && max > 0 {
		lines = lines[len(lines)-max:]
	}
	for i, line := range lines {
		yy := a.logR.Y + 4 + i*16
		if yy+16 > a.logR.Bottom()-2 {
			break
		}
		col := ui.ColText
		if strings.HasPrefix(line, "失败") {
			col = ui.ColRed
		}
		f.TextClip(c, a.logR.X+4, yy, a.logR.W-8, line, col)
	}

	btnY := r.Bottom() - 30
	a.closeR = ui.Rect{right - 76, btnY, 76, 24}
	a.rebootR = ui.Rect{a.closeR.X - 6 - 88, btnY, 88, 24}
	a.cancelR = ui.Rect{a.rebootR.X - 6 - 76, btnY, 76, 24}
	a.installR = ui.Rect{a.cancelR.X - 6 - 110, btnY, 110, 24}
	a.checkR = ui.Rect{a.installR.X - 6 - 88, btnY, 88, 24}

	ui.Button(c, a.closeR, "关闭", a.press == "close", true)
	switch {
	case a.busy():
		ui.Button(c, a.cancelR, "取消", a.press == "cancel", a.phase != otaApplying)
		if a.phase == otaApplying {
			ui.Button(c, a.installR, "正在安装…", false, false)
		}
	case a.phase == otaReady:
		ui.Button(c, a.rebootR, "立即重启", a.press == "reboot", true)
	default:
		ui.Button(c, a.checkR, "检查更新", a.press == "check", true)
		ui.Button(c, a.installR, "下载并安装", a.press == "install", a.canInstall())
	}
}

func (a *OTAApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.closeR.Contains(ev.X, ev.Y):
			a.press = "close"
		case a.rebootR.Contains(ev.X, ev.Y):
			a.press = "reboot"
		case a.cancelR.Contains(ev.X, ev.Y):
			a.press = "cancel"
		case a.installR.Contains(ev.X, ev.Y):
			a.press = "install"
		case a.checkR.Contains(ev.X, ev.Y):
			a.press = "check"
		}
		if a.press != "" {
			d.Invalidate()
		}
	case MouseUp:
		press := a.press
		a.press = ""
		switch press {
		case "close":
			if a.closeR.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		case "reboot":
			if a.rebootR.Contains(ev.X, ev.Y) && a.phase == otaReady {
				a.reboot()
			}
		case "cancel":
			if a.cancelR.Contains(ev.X, ev.Y) && a.busy() && a.phase != otaApplying {
				a.cancelRun()
			}
		case "install":
			if a.installR.Contains(ev.X, ev.Y) {
				a.startInstall()
			}
		case "check":
			if a.checkR.Contains(ev.X, ev.Y) {
				a.startCheck()
			}
		}
		d.Invalidate()
	}
}

func (a *OTAApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl {
		return false
	}
	switch ev.Name {
	case "F5":
		a.startCheck()
		return true
	case "Enter":
		if a.phase == otaReady {
			a.reboot()
		} else if a.canInstall() {
			a.startInstall()
		}
		return true
	}
	return false
}
