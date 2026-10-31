package desktop

import (
	"os"
	"path/filepath"
	"strings"

	"win95linux/internal/ui"
)

// fileDrag is the ghost shown while a file is dragged out of a file manager
// window. The source app owns the drop handling; the desktop only paints it.
type fileDrag struct {
	name string
	path string
}

const (
	dropNone = iota
	dropRecycle
	dropDesktop
)

func (d *Desktop) fileDragTarget(x, y int) int {
	if idx := d.iconAt(x, y); idx >= 0 && d.icons[idx].key == "sys:recycle" {
		return dropRecycle
	}
	if w := d.windowAt(x, y); w != nil {
		if fm, ok := w.App.(*FileManagerApp); ok && fm.path == trashDir {
			return dropRecycle
		}
		return dropNone
	}
	if y >= d.taskbarTop() {
		return dropNone
	}
	return dropDesktop
}

func (d *Desktop) drawFileDrag(c *ui.Canvas) {
	if d.fileDrag == nil || d.fileDrag.path == "" {
		return
	}
	f := ui.DefaultFont()
	gx, gy := d.MouseX+8, d.MouseY+6
	kind := desktopFileIcon(d.fileDrag.name)
	c.FillRect(ui.Rect{gx + 2, gy + 2, 36, 36}, ui.ColShadow)
	c.FillRect(ui.Rect{gx, gy, 36, 36}, ui.ColFace)
	c.Raised(ui.Rect{gx, gy, 36, 36})
	ui.DrawIcon32(c, kind, gx+2, gy+2, false)

	label := d.fileDrag.name
	tw := f.Width(label)
	lx := gx + 18 - tw/2
	ly := gy + 40
	if lx < 2 {
		lx = 2
	}
	if lx+tw > d.W-2 {
		lx = d.W - 2 - tw
	}
	c.FillRect(ui.Rect{lx - 2, ly - 1, tw + 4, 18}, ui.ColNavy)
	f.Text(c, lx, ly, label, ui.ColWhite)

	target := d.fileDragTarget(d.MouseX, d.MouseY)
	hint := ""
	switch target {
	case dropRecycle:
		hint = "松开鼠标放入回收站"
	case dropDesktop:
		hint = "松开鼠标在桌面创建快捷方式"
	}
	if hint == "" {
		return
	}
	hw := f.Width(hint) + 12
	hx := gx
	hy := gy + 62
	if hy+22 > d.taskbarTop()-2 {
		hy = gy - 26
	}
	if hx+hw > d.W-4 {
		hx = d.W - 4 - hw
	}
	c.FillRect(ui.Rect{hx, hy, hw, 20}, 0xFFFFE1)
	c.Frame(ui.Rect{hx, hy, hw, 20}, ui.ColDark)
	f.Text(c, hx+6, hy+4, hint, ui.ColText)
}

func (d *Desktop) dropFileDrag(x, y int) {
	drag := d.fileDrag
	d.fileDrag = nil
	if drag == nil || drag.path == "" {
		return
	}
	switch d.fileDragTarget(x, y) {
	case dropRecycle:
		if _, err := moveToTrash(drag.path); err != nil {
			d.MessageBox("回收站", "无法把“"+drag.name+"”放入回收站：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		d.refreshShortcuts(true)
		d.Notify("回收站", "“"+drag.name+"”已放入回收站。")
		d.Invalidate()
	case dropDesktop:
		target, err := d.createDesktopShortcut(drag.path)
		if err != nil {
			d.MessageBox("桌面", "无法创建快捷方式：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		d.Notify("桌面", "已创建快捷方式“"+filepath.Base(target)+"”。")
		d.Invalidate()
	}
}

// createDesktopShortcut links a file or folder onto the Win95 desktop. The
// name keeps its extension so double-clicking opens it like the original.
func (d *Desktop) createDesktopShortcut(path string) (string, error) {
	if err := os.MkdirAll(shortcutsDir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stem = sanitizeShortcutName(stem + " - 快捷方式")
	name := uniqueDesktopName(shortcutsDir, stem, ext)
	target := filepath.Join(shortcutsDir, name)
	if err := os.Symlink(path, target); err != nil {
		return "", err
	}
	d.refreshShortcuts(true)
	d.selectIconPath(target)
	return target, nil
}
