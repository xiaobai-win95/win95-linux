package desktop

import (
	"os"

	"win95linux/internal/ui"
)

type iconPoint struct {
	x, y int
}

type desktopIcon struct {
	label  string
	kind   ui.IconKind
	key    string
	path   string
	isDir  bool
	action func()
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
		{label: "我的电脑", kind: ui.IcoComputer, key: "sys:computer", action: func() { d.openFiles("") }},
		{label: "我的文档", kind: ui.IcoFolder, key: "sys:docs", isDir: true, action: func() { d.openFiles("/root/文档") }},
		{label: "网络浏览器", kind: ui.IcoBrowser, key: "sys:browser", action: func() { d.openBrowser("") }},
		{label: "XFCE 桌面", kind: ui.IcoComputer, key: "sys:xfce", action: func() { d.switchToXFCE() }},
		{label: "回收站", kind: ui.IcoRecycle, key: "sys:recycle", action: func() { d.openRecycle() }},
	}
	d.baseIcons = append([]*desktopIcon{}, d.icons...)
	d.refreshShortcuts(true)
}

func (d *Desktop) openRecycle() {
	if st, err := os.Stat("/root/.trash"); err == nil && st.IsDir() {
		d.openFiles("/root/.trash")
		return
	}
	d.MessageBox("回收站", "回收站是空的。\n\n删除的文件会暂时保存在这里。", IconInfo, []string{"确定"}, nil)
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
	d.Open(a)
}

func (d *Desktop) openFiles(path string) {
	a := NewFileManagerApp(d, path)
	w := d.Open(a)
	a.win = w
}

func (d *Desktop) openBrowser(url string) {
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok {
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

func (d *Desktop) openMines() {
	for _, w := range d.windows {
		if _, ok := w.App.(*MineApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewMineApp())
}

func (d *Desktop) openPaint() {
	for _, w := range d.windows {
		if _, ok := w.App.(*PaintApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewPaintApp(d))
}
