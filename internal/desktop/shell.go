package desktop

import (
	"fmt"
	"os"
	"path/filepath"

	"win95linux/internal/ui"
)

const trashDir = "/tmp/.trash"

type iconPoint struct {
	x, y int
}

type desktopIcon struct {
	label    string
	labelKey string
	kind     ui.IconKind
	key      string
	path     string
	isDir    bool
	action   func()
}

func (d *Desktop) setupShell() {
	if d.Mode == ModeInstaller {
		d.icons = []*desktopIcon{
			{label: "安装程序", kind: ui.IcoSetup, key: "sys:setup", action: func() { d.focusInstaller() }},
			{label: "我的电脑", kind: ui.IcoComputer, key: "sys:computer", action: func() { d.openFiles("") }},
			{label: "安装说明", kind: ui.IcoText, key: "sys:setuphelp", action: func() { d.showInstallHelp() }},
			{label: "回收站", kind: ui.IcoRecycle, key: "sys:recycle", action: func() { d.openRecycle() }},
		}
		d.installer = NewInstallerApp(d)
		w := d.Open(d.installer)
		d.installer.win = w
		w.X = (d.W - w.W) / 2
		w.Y = (d.taskbarTop() - w.H) / 2
		return
	}
	d.icons = []*desktopIcon{
		{labelKey: "我的电脑", label: d.tr("我的电脑"), kind: ui.IcoComputer, key: "sys:computer", action: func() { d.openFiles("") }},
		{labelKey: "网上邻居", label: d.tr("网上邻居"), kind: ui.IcoNetwork, key: "sys:network", action: func() { d.openLANShare() }},
		{labelKey: "收件箱", label: d.tr("收件箱"), kind: ui.IcoMail, key: "sys:inbox", action: func() { d.openInbox() }},
		{label: "Internet Explorer", kind: ui.IcoIE, key: "sys:ie", action: func() { d.openBrowser("about:home") }},
		{label: "Netscape Navigator", kind: ui.IcoNetscape, key: "sys:netscape", action: func() { d.openNetscape() }},
		{labelKey: "回收站", label: d.tr("回收站"), kind: ui.IcoRecycle, key: "sys:recycle", action: func() { d.openRecycle() }},
		{labelKey: "我的公文包", label: d.tr("我的公文包"), kind: ui.IcoBriefcase, key: "sys:briefcase", action: func() { d.openBriefcase() }},
		{labelKey: "游戏", label: d.tr("游戏"), kind: ui.IcoGame, key: "sys:games", action: func() { d.openGames() }},
		{labelKey: "在线服务", label: d.tr("在线服务"), kind: ui.IcoOnline, key: "sys:online", action: func() { d.openOnlineServices() }},
		{label: "尝试 The Microsoft Network", kind: ui.IcoMSN, key: "sys:msn", action: func() { d.openMSN() }},
		{label: "AI 助手", kind: ui.IcoAI, key: "sys:ai", action: func() { d.openAI(0) }},
		{labelKey: "切换到 XFCE 桌面", label: d.tr("切换到 XFCE 桌面"), kind: ui.IcoSetup, key: "sys:xfce", action: func() { d.switchToXFCE() }},
		{labelKey: "设置中心", label: d.tr("设置中心"), kind: ui.IcoSetup, key: "sys:settings", action: func() { d.openSettings() }},
	}
	d.baseIcons = append([]*desktopIcon{}, d.icons...)
	d.refreshShortcuts(true)
}

func (d *Desktop) openTaskManager() *TaskManagerApp {
	for _, w := range d.windows {
		if a, ok := w.App.(*TaskManagerApp); ok {
			d.bringToFront(w)
			return a
		}
	}
	a := NewTaskManagerApp(d)
	d.Open(a)
	return a
}

func (d *Desktop) openRecycle() {
	if st, err := os.Stat(trashDir); err == nil && st.IsDir() {
		d.openFiles(trashDir)
		return
	}
	d.MessageBox("回收站", "回收站是空的。\n\n删除的文件会暂时保存在这里。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) recycleCount() int {
	ents, err := os.ReadDir(trashDir)
	if err != nil {
		return 0
	}
	return len(ents)
}

func (d *Desktop) emptyRecycle(after func()) {
	ents, err := os.ReadDir(trashDir)
	if err != nil || len(ents) == 0 {
		d.MessageBox("回收站", "回收站已经是空的。", IconInfo, []string{"确定"}, nil)
		if after != nil {
			after()
		}
		return
	}
	d.MessageBox("确认删除", fmt.Sprintf("确实要删除回收站中的 %d 个项目吗？\n\n这些项目将被永久删除。", len(ents)),
		IconQuestion, []string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			failed := ""
			for _, e := range ents {
				if err := os.RemoveAll(filepath.Join(trashDir, e.Name())); err != nil {
					failed = err.Error()
				}
			}
			d.refreshShortcuts(true)
			d.Invalidate()
			if after != nil {
				after()
			}
			if failed != "" {
				d.MessageBox("回收站", "无法删除所有项目：\n"+failed, IconError, []string{"确定"}, nil)
				return
			}
			d.MessageBox("回收站", "回收站已清空。", IconInfo, []string{"确定"}, nil)
		})
}

func (d *Desktop) openNotepad(path string) {
	a := NewNotepadApp(d, path)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openCalc() {
	for _, w := range d.windows {
		if _, ok := w.App.(*CalcApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewCalcApp())
}

func (d *Desktop) openCalendar() {
	for _, w := range d.windows {
		if _, ok := w.App.(*CalendarApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewCalendarApp())
}

func (d *Desktop) openTerminal() {
	a := NewTerminalApp(d)
	a.win = d.Open(a)
}

// openTerminalCommand runs argv in an interactive PTY terminal. A non-empty
// key focuses an already running terminal instead of opening a duplicate.
func (d *Desktop) openTerminalCommand(title string, argv []string, cwd, key string) *TerminalApp {
	if key != "" {
		for _, w := range d.windows {
			if t, ok := w.App.(*TerminalApp); ok && t.key == key && !t.t.closed {
				d.bringToFront(w)
				return t
			}
		}
	}
	a := newTerminalApp(d, termOptions{argv: argv, cwd: cwd}, title, key)
	a.win = d.Open(a)
	return a
}

// openTerminalAt starts a shell in the given directory.
func (d *Desktop) openTerminalAt(path string) {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		path = "/root"
	}
	title := "终端"
	if base := filepath.Base(path); base != "/" && base != "." {
		title = "终端 - " + base
	}
	a := newTerminalApp(d, termOptions{cwd: path}, title, "")
	a.win = d.Open(a)
}

func (d *Desktop) openPowerShell() {
	a := NewPShellApp(d)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openISE() {
	for _, w := range d.windows {
		if ise, ok := w.App.(*PShellISEApp); ok {
			d.bringToFront(w)
			_ = ise
			return
		}
	}
	a := NewPShellISEApp(d)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openFiles(path string) {
	a := NewFileManagerApp(d, path)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openImageViewer(path string) {
	for _, w := range d.windows {
		if v, ok := w.App.(*ImageViewerApp); ok {
			if path != "" {
				v.loadPath(path)
			}
			d.bringToFront(w)
			d.Invalidate()
			return
		}
	}
	a := NewImageViewerApp(d)
	if path != "" {
		a.loadPath(path)
	}
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openBrowser(url string) {
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok && b.brand != netscapeBrand {
			d.bringToFront(w)
			if url != "" {
				b.Navigate(url)
			}
			return
		}
	}
	b := NewBrowserApp(d, url)
	w := d.Open(b)
	b.win = w
}

func (d *Desktop) openPkgMan() {
	for _, w := range d.windows {
		if _, ok := w.App.(*PkgManApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewPkgManApp(d))
}

func (d *Desktop) openOTA() {
	for _, w := range d.windows {
		if _, ok := w.App.(*OTAApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewOTAApp(d))
}

func (d *Desktop) openMines() {
	for _, w := range d.windows {
		if _, ok := w.App.(*MineApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewMineApp())
}

func (d *Desktop) openBlockWorld() {
	for _, w := range d.windows {
		if _, ok := w.App.(*BlockWorldApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewBlockWorldApp(d)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openPaint() {
	for _, w := range d.windows {
		if _, ok := w.App.(*PaintApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewPaintApp(d)
	w := d.Open(a)
	a.win = w
}
