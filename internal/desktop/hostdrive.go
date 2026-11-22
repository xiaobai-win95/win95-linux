package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"win95linux/internal/ui"
)

var hostDrivesFile = "/etc/win95-hostdrives.json"

type hostDrive struct {
	Letter string `json:"letter"`
	Path   string `json:"path"`
}

func loadHostDrives() []hostDrive {
	b, err := os.ReadFile(hostDrivesFile)
	if err != nil {
		return nil
	}
	var list []hostDrive
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	out := list[:0]
	for _, hd := range list {
		letter, err := normalizeDriveLetter(hd.Letter)
		if err != nil {
			continue
		}
		hd.Letter = letter
		if hd.Path = strings.TrimSpace(hd.Path); hd.Path == "" {
			continue
		}
		out = append(out, hd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Letter < out[j].Letter })
	return out
}

func saveHostDrives(list []hostDrive) error {
	sort.Slice(list, func(i, j int) bool { return list[i].Letter < list[j].Letter })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hostDrivesFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(hostDrivesFile, append(b, '\n'), 0o644)
}

func normalizeDriveLetter(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(s, ":")))
	if s == "C" {
		return "", fmt.Errorf("C: 是系统盘，请选择其他盘符")
	}
	if len(s) != 1 || s[0] < 'A' || s[0] > 'Z' {
		return "", fmt.Errorf("盘符必须是 A–Z 中的一个字母（C 除外）")
	}
	return s, nil
}

func addHostDrive(letter, path string) (hostDrive, error) {
	lt, err := normalizeDriveLetter(letter)
	if err != nil {
		return hostDrive{}, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return hostDrive{}, fmt.Errorf("请输入宿主机路径")
	}
	if !filepath.IsAbs(path) {
		return hostDrive{}, fmt.Errorf("请使用绝对路径，例如 /Volumes/Shared 挂载到虚拟机后的 /mnt/share")
	}
	st, err := os.Stat(path)
	if err != nil {
		return hostDrive{}, fmt.Errorf("无法访问路径：%v", err)
	}
	if !st.IsDir() {
		return hostDrive{}, fmt.Errorf("该路径不是文件夹")
	}
	list := loadHostDrives()
	replaced := false
	for i := range list {
		if list[i].Letter == lt {
			list[i].Path = path
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, hostDrive{Letter: lt, Path: path})
	}
	if err := saveHostDrives(list); err != nil {
		return hostDrive{}, err
	}
	return hostDrive{Letter: lt, Path: path}, nil
}

func removeHostDrive(letter string) error {
	lt, err := normalizeDriveLetter(letter)
	if err != nil {
		return err
	}
	list := loadHostDrives()
	out := list[:0]
	for _, hd := range list {
		if hd.Letter != lt {
			out = append(out, hd)
		}
	}
	return saveHostDrives(out)
}

func detectedHostShares() []fmShare {
	return fmSharedMounts()
}

func (a *FileManagerApp) disconnectHostDrive(d *Desktop, letter string) {
	upath := ""
	for _, hd := range loadHostDrives() {
		if hd.Letter == letter {
			upath = hd.Path
			break
		}
	}
	d.MessageBox("断开驱动器", "确定要断开 "+letter+": 吗？\n\n文件本身不会被删除，只是把盘符从“我的电脑”移除。",
		IconQuestion, []string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			if err := removeHostDrive(letter); err != nil {
				d.MessageBox("断开失败", err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			if upath != "" && (a.path == upath || strings.HasPrefix(a.path, upath+string(filepath.Separator))) {
				a.navigate(d, "")
			} else {
				a.reload(true)
			}
			d.Notify("驱动器已断开", letter+": 已从“我的电脑”移除")
		})
}

type HostDriveDialog struct {
	Base
	d   *Desktop
	win *Window

	letter []rune
	path   []rune
	caret  int
	focus  int

	shares     []fmShare
	shareRects []ui.Rect
	errLine    string
	after      func()
	press      string

	letterR, pathR, btnMap, btnCancel ui.Rect
}

func (d *Desktop) openHostDriveDialog(after func()) *HostDriveDialog {
	a := &HostDriveDialog{d: d, letter: []rune("Z"), after: after}
	a.shares = detectedHostShares()
	if len(a.shares) > 0 {
		a.path = []rune(a.shares[0].mount)
	}
	a.caret = len(a.path)
	w := d.OpenModal(a)
	w.W, w.H = 560, 330
	w.MinW, w.MinH = w.W, w.H
	w.X = (d.W - w.W) / 2
	w.Y = (d.taskbarTop() - w.H) / 2
	a.win = w
	return a
}

func (a *HostDriveDialog) Title() string           { return "映射宿主机文件夹" }
func (a *HostDriveDialog) Icon() ui.IconKind       { return ui.IcoDrive }
func (a *HostDriveDialog) DefaultSize() (int, int) { return 560, 330 }
func (a *HostDriveDialog) MinSize() (int, int)     { return 560, 330 }
func (a *HostDriveDialog) Resizable() bool         { return false }

func (a *HostDriveDialog) paintField(c *ui.Canvas, f *ui.Font, r ui.Rect, value []rune, caret int, focused bool) {
	c.FillRect(r, ui.ColWhite)
	c.Sunken(r)
	if len(value) > 0 {
		f.TextClip(c, r.X+5, r.Y+4, r.W-10, string(value), ui.ColText)
	}
	if focused && caret >= 0 && caret <= len(value) {
		cx := r.X + 5 + f.Width(string(value[:caret]))
		if cx < r.Right()-3 {
			c.VLine(cx, r.Y+4, 16, ui.ColText)
		}
	}
}

func (a *HostDriveDialog) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.Text(c, r.X+16, r.Y+14, "把 Mac 宿主机通过 UTM 共享出来的文件夹映射成一个盘符，", ui.ColText)
	f.Text(c, r.X+16, r.Y+30, "之后可在“我的电脑”里像本地磁盘一样读写。", ui.ColText)

	f.Text(c, r.X+16, r.Y+58, "驱动器号:", ui.ColText)
	a.letterR = ui.Rect{r.X + 86, r.Y + 54, 48, 24}
	a.paintField(c, f, a.letterR, a.letter, len(a.letter), a.focus == 0)

	f.Text(c, r.X+16, r.Y+92, "宿主机路径:", ui.ColText)
	a.pathR = ui.Rect{r.X + 16, r.Y + 106, r.W - 32, 24}
	a.paintField(c, f, a.pathR, a.path, a.caret, a.focus == 1)
	f.Text(c, r.X+16, r.Y+134, "提示：在 UTM 里先把 Mac 文件夹设为共享目录（VirtioFS/9p），再填写它在虚拟机中的挂载路径。", ui.ColShadow)

	listTop := r.Y + 158
	listBottom := r.Bottom() - 46
	c.FillRect(ui.Rect{r.X + 16, listTop, r.W - 32, 17}, ui.ColFace)
	f.Text(c, r.X+16, listTop, "检测到的共享文件夹（单击填入）：", ui.ColText)
	list := ui.Rect{r.X + 16, listTop + 18, r.W - 32, listBottom - listTop - 18}
	c.FillRect(list, ui.ColWhite)
	c.Sunken(list)
	a.shareRects = a.shareRects[:0]
	if len(a.shares) == 0 {
		f.Text(c, list.X+7, list.Y+5, "未检测到 VirtioFS / 9p 共享。仍可手动输入路径。", ui.ColShadow)
	} else {
		y := list.Y + 3
		for _, sh := range a.shares {
			if y+20 > list.Bottom() {
				break
			}
			rr := ui.Rect{list.X + 2, y, list.W - 4, 20}
			a.shareRects = append(a.shareRects, rr)
			f.TextClip(c, rr.X+5, rr.Y+2, rr.W-10, sh.mount+"    ("+sh.fstype+")", ui.ColText)
			y += 21
		}
	}
	if a.errLine != "" {
		f.TextClip(c, r.X+16, r.Bottom()-24, r.W-32, a.errLine, ui.ColRed)
	}

	a.btnMap = ui.Rect{r.Right() - 196, r.Bottom() - 34, 86, 26}
	a.btnCancel = ui.Rect{r.Right() - 102, r.Bottom() - 34, 86, 26}
	ui.Button(c, a.btnMap, "映射", a.press == "map", true)
	ui.Button(c, a.btnCancel, "取消", a.press == "cancel", true)
	_ = f
}

func (a *HostDriveDialog) applyMapping() {
	hd, err := addHostDrive(string(a.letter), string(a.path))
	if err != nil {
		a.errLine = err.Error()
		a.d.Invalidate()
		return
	}
	a.d.Notify("映射宿主机文件夹", fmt.Sprintf("%s: → %s", hd.Letter, hd.Path))
	a.d.CloseWindow(a.win)
	if a.after != nil {
		a.after()
	}
}

func (a *HostDriveDialog) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if ev.Kind == MouseMove {
		return
	}
	if ev.Kind == MouseUp {
		p := a.press
		a.press = ""
		if ev.Button != 1 {
			return
		}
		switch p {
		case "map":
			if a.btnMap.Contains(ev.X, ev.Y) {
				a.applyMapping()
			}
		case "cancel":
			if a.btnCancel.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
		return
	}
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	a.press = ""
	switch {
	case a.letterR.Contains(ev.X, ev.Y):
		a.focus = 0
	case a.pathR.Contains(ev.X, ev.Y):
		a.focus = 1
		a.caret = runCaretAt(ui.DefaultFont(), a.path, a.pathR, ev.X)
	case a.btnMap.Contains(ev.X, ev.Y):
		a.press = "map"
	case a.btnCancel.Contains(ev.X, ev.Y):
		a.press = "cancel"
	default:
		for i, rr := range a.shareRects {
			if rr.Contains(ev.X, ev.Y) && i < len(a.shares) {
				a.path = []rune(a.shares[i].mount)
				a.caret = len(a.path)
				a.focus = 1
				break
			}
		}
	}
	d.Invalidate()
}

func (a *HostDriveDialog) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	a.win = w
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "Tab":
		a.focus = (a.focus + 1) % 2
		return true
	case "Enter":
		a.applyMapping()
		return true
	}
	if a.focus == 0 {
		if ev.Rune >= 'a' && ev.Rune <= 'z' {
			ev.Rune -= 'a' - 'A'
		}
		if ev.Rune >= 'A' && ev.Rune <= 'Z' {
			a.letter = []rune{ev.Rune}
			return true
		}
		return false
	}
	value := &a.path
	switch ev.Name {
	case "Backspace":
		if a.caret > 0 {
			*value = append((*value)[:a.caret-1], (*value)[a.caret:]...)
			a.caret--
		}
		return true
	case "Delete":
		if a.caret < len(*value) {
			*value = append((*value)[:a.caret], (*value)[a.caret+1:]...)
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
		}
		return true
	case "Right":
		if a.caret < len(*value) {
			a.caret++
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(*value)
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		*value = append(*value, 0)
		copy((*value)[a.caret+1:], (*value)[a.caret:])
		(*value)[a.caret] = ev.Rune
		a.caret++
		return true
	}
	return false
}

func (a *HostDriveDialog) Close() {}
