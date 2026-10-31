package desktop

import (
	"os"
	"path/filepath"
	"strings"

	"win95linux/internal/ui"
)

const runPrompt = "键入程序、文件夹、文档或 Internet 资源的名称，Windows 将为您打开它。"

type RunDialogApp struct {
	Base
	d      *Desktop
	win    *Window
	value  []rune
	caret  int
	hist   int
	draft  []rune
	press  string
	err    string
	fieldR ui.Rect
	okR    ui.Rect
	cancel ui.Rect
	browse ui.Rect
}

func (d *Desktop) openRunDialog() {
	a := &RunDialogApp{d: d, hist: -1}
	a.win = d.OpenModal(a)
}

func (a *RunDialogApp) Title() string           { return "运行" }
func (a *RunDialogApp) Icon() ui.IconKind       { return ui.IcoSetup }
func (a *RunDialogApp) DefaultSize() (int, int) { return 480, 224 }
func (a *RunDialogApp) MinSize() (int, int)     { return 480, 224 }
func (a *RunDialogApp) Resizable() bool         { return false }

func (a *RunDialogApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	ui.DrawIcon32(c, ui.IcoSetup, r.X+16, r.Y+14, false)
	y := r.Y + 14
	for _, line := range wrapText(runPrompt, r.W-100) {
		f.Text(c, r.X+64, y, line, ui.ColText)
		y += 20
	}
	f.Text(c, r.X+16, r.Y+66, "打开(O):", ui.ColText)
	a.fieldR = ui.Rect{r.X + 16, r.Y + 84, r.W - 32, 26}
	c.FillRect(a.fieldR, ui.ColWhite)
	c.Sunken(a.fieldR)
	f.TextClip(c, a.fieldR.X+5, a.fieldR.Y+5, a.fieldR.W-10, string(a.value), ui.ColText)
	before := string(a.value[:a.caret])
	cx := a.fieldR.X + 5 + f.Width(before)
	if cx < a.fieldR.Right()-4 {
		c.VLine(cx, a.fieldR.Y+4, 18, ui.ColText)
	}
	if a.err != "" {
		f.TextClip(c, r.X+16, r.Y+116, r.W-32, a.err, ui.ColRed)
	} else {
		f.Text(c, r.X+16, r.Y+118, "提示：按 ↑ ↓ 可调出最近使用的命令", ui.ColShadow)
	}

	a.browse = ui.Rect{r.X + 16, r.Bottom() - 38, 92, 26}
	a.okR = ui.Rect{r.Right() - 196, r.Bottom() - 38, 84, 26}
	a.cancel = ui.Rect{r.Right() - 104, r.Bottom() - 38, 84, 26}
	ui.Button(c, a.browse, "浏览(B)…", a.press == "browse", true)
	ui.Button(c, a.okR, "确定", a.press == "ok", true)
	ui.Button(c, a.cancel, "取消", a.press == "cancel", true)
}

func (a *RunDialogApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.okR.Contains(ev.X, ev.Y):
			a.press = "ok"
		case a.cancel.Contains(ev.X, ev.Y):
			a.press = "cancel"
		case a.browse.Contains(ev.X, ev.Y):
			a.press = "browse"
		case a.fieldR.Contains(ev.X, ev.Y):
			a.caret = runCaretAt(ui.DefaultFont(), a.value, a.fieldR, ev.X)
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "ok":
			if a.okR.Contains(ev.X, ev.Y) {
				a.accept()
			}
		case "cancel":
			if a.cancel.Contains(ev.X, ev.Y) {
				a.finish()
			}
		case "browse":
			if a.browse.Contains(ev.X, ev.Y) {
				a.browseFile()
			}
		}
	}
}

func runCaretAt(f *ui.Font, value []rune, field ui.Rect, x int) int {
	rel := x - (field.X + 5)
	if rel <= 0 {
		return 0
	}
	width := 0
	for i, r := range value {
		w := f.RuneWidth(r)
		if width+w/2 >= rel {
			return i
		}
		width += w
	}
	return len(value)
}

func (a *RunDialogApp) setValue(s string) {
	a.value = []rune(s)
	a.caret = len(a.value)
	a.err = ""
}

func (a *RunDialogApp) browseFile() {
	a.d.FileDialog(FileOpen, "/root", "", func(path string, ok bool) {
		if !ok {
			return
		}
		a.setValue(path)
		a.d.Invalidate()
	})
}

func (a *RunDialogApp) accept() {
	cmd := strings.TrimSpace(string(a.value))
	if cmd == "" {
		a.err = "请输入要运行的程序、文件夹或文档名称。"
		a.d.Invalidate()
		return
	}
	if !a.d.runCommand(cmd) {
		a.err = "Windows 找不到“" + cmd + "”。请检查名称后重试。"
		a.d.Invalidate()
		return
	}
	a.d.addRunHistory(cmd)
	a.finish()
}

func (a *RunDialogApp) finish() {
	win := a.win
	if win != nil {
		a.d.CloseWindowForced(win)
	}
}

func (a *RunDialogApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		a.finish()
		return true
	case "Enter":
		a.accept()
		return true
	case "Backspace":
		if a.caret > 0 {
			a.value = append(a.value[:a.caret-1], a.value[a.caret:]...)
			a.caret--
			a.hist = -1
			a.err = ""
		}
		return true
	case "Delete":
		if a.caret < len(a.value) {
			a.value = append(a.value[:a.caret], a.value[a.caret+1:]...)
			a.hist = -1
			a.err = ""
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
		}
		return true
	case "Right":
		if a.caret < len(a.value) {
			a.caret++
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(a.value)
		return true
	case "Up":
		if len(d.runHistory) > 0 {
			if a.hist < 0 {
				a.draft = append([]rune{}, a.value...)
			}
			if a.hist < len(d.runHistory)-1 {
				a.hist++
			}
			a.setValue(d.runHistory[a.hist])
		}
		return true
	case "Down":
		if a.hist >= 0 {
			a.hist--
			if a.hist < 0 {
				a.value = append([]rune{}, a.draft...)
				a.caret = len(a.value)
			} else {
				a.setValue(d.runHistory[a.hist])
			}
		}
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		a.value = append(a.value, 0)
		copy(a.value[a.caret+1:], a.value[a.caret:])
		a.value[a.caret] = ev.Rune
		a.caret++
		a.hist = -1
		a.err = ""
		return true
	}
	return false
}

func (a *RunDialogApp) Close() {}

func expandRunPath(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "\"'"))
	if s == "~" {
		return "/root"
	}
	if strings.HasPrefix(s, "~/") {
		return filepath.Join("/root", s[2:])
	}
	if filepath.IsAbs(s) {
		return s
	}
	if _, err := os.Stat(s); err == nil {
		return s
	}
	return filepath.Join("/root", s)
}

func runURL(s string) (string, bool) {
	lower := strings.ToLower(s)
	for _, scheme := range []string{"http://", "https://", "file://"} {
		if strings.HasPrefix(lower, scheme) {
			return s, true
		}
	}
	if strings.ContainsAny(s, " \t\\") {
		return "", false
	}
	host := s
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if !strings.Contains(host, ".") {
		return "", false
	}
	tld := strings.ToLower(host[strings.LastIndexByte(host, '.')+1:])
	switch tld {
	case "com", "org", "net", "cn", "io", "dev", "edu", "gov", "app", "me", "xyz", "uk", "jp", "de", "top":
		return "https://" + s, true
	}
	return "", false
}

// runCommand opens a built-in program, a file, a folder or a URL. It reports
// false when the input does not resolve to anything the shell can open.
func (d *Desktop) runCommand(input string) bool {
	cmd := strings.TrimSpace(strings.Trim(input, "\"'"))
	if cmd == "" {
		return false
	}
	fields := strings.Fields(cmd)
	head := strings.ToLower(fields[0])
	head = strings.TrimSuffix(head, ".exe")
	arg := strings.TrimSpace(strings.TrimPrefix(cmd, fields[0]))
	arg = strings.TrimSpace(strings.Trim(arg, "\"'"))

	switch head {
	case "notepad", "记事本":
		if arg != "" {
			p := expandRunPath(arg)
			if _, err := os.Stat(p); err != nil {
				return false
			}
			d.openNotepad(p)
		} else {
			d.openNotepad("")
		}
		return true
	case "calc", "计算器":
		d.openCalc()
		return true
	case "calendar", "日历":
		d.openCalendar()
		return true
	case "cmd", "command", "terminal", "终端":
		d.openTerminal()
		return true
	case "explorer", "files", "文件管理器":
		p := "/root"
		if arg != "" {
			p = expandRunPath(arg)
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				return false
			}
		}
		d.openFiles(p)
		return true
	case "iexplore", "browser", "浏览器":
		url := arg
		d.openBrowser(url)
		return true
	case "pkgman", "store", "软件包管理器":
		d.openPkgMan()
		return true
	case "mines", "winmine", "扫雷":
		d.openMines()
		return true
	case "paint", "mspaint", "画图":
		d.openPaint()
		return true
	case "sol", "solitaire", "纸牌":
		d.openSolitaire()
		return true
	case "wget", "download", "downloader", "下载器":
		d.openDownloader()
		return true
	case "firefox":
		d.launchXFCEApp("firefox")
		return true
	case "xfce4-terminal", "xfce-terminal", "xfce-term":
		d.launchXFCEApp("xfce4-terminal")
		return true
	case "control", "display", "显示属性":
		d.openDisplayProperties()
		return true
	case "find", "查找":
		d.openFind()
		return true
	case "winver", "sysinfo", "系统信息", "关于":
		d.showAbout()
		return true
	case "recycle", "回收站":
		d.openRecycle()
		return true
	case "shutdown", "关闭计算机":
		d.showShutdown()
		return true
	case "logoff", "注销":
		d.showLogoff()
		return true
	case "restart", "重新启动":
		d.showRestart()
		return true
	}

	p := expandRunPath(cmd)
	if st, err := os.Stat(p); err == nil {
		if st.IsDir() {
			d.openFiles(p)
		} else {
			d.openDesktopPath(p)
		}
		return true
	}
	if u, ok := runURL(cmd); ok {
		d.openBrowser(u)
		return true
	}
	return false
}
