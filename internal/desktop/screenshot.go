package desktop

import "strings"

// takeScreenshot asks the host to encode the current framebuffer frame as a
// PNG and a BMP, then reports both saved files in a balloon.
func (d *Desktop) takeScreenshot() {
	if d.Host.Screenshot == nil {
		d.MessageBox("截屏", "预览模式无法保存截图。\n\n在虚拟机中按 Print Screen 即可。", IconInfo, []string{"确定"}, nil)
		return
	}
	path, err := d.Host.Screenshot()
	if err != nil {
		d.MessageBox("截屏", "无法保存截图：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	text := path
	if parts := strings.SplitN(path, "\n", 2); len(parts) == 2 {
		text = "PNG：" + parts[0] + "\nBMP：" + parts[1]
	}
	d.Notify("截图已保存（PNG + BMP）", text)
}

// printScreenToPaint saves the current frame to the desktop and loads the PNG
// into Paint for annotation (brush, selection, mosaic).
func (d *Desktop) printScreenToPaint() {
	if d.Host.Screenshot == nil {
		d.MessageBox("截屏", "预览模式无法截屏。\n\n在虚拟机中按 Print Screen 即可。", IconInfo, []string{"确定"}, nil)
		return
	}
	path, err := d.Host.Screenshot()
	if err != nil {
		d.MessageBox("截屏", "无法保存截图：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	pngPath := strings.SplitN(path, "\n", 2)[0]
	text := "PNG：" + pngPath
	if parts := strings.SplitN(path, "\n", 2); len(parts) == 2 {
		text += "\nBMP：" + parts[1]
	}
	d.Notify("截图已保存到桌面，可在画图中编辑", text)
	d.openPaintImage(pngPath)
}

func (d *Desktop) openPaintImage(path string) {
	d.openPaint()
	for _, w := range d.windows {
		if a, ok := w.App.(*PaintApp); ok {
			d.bringToFront(w)
			a.openPath(d, path)
			return
		}
	}
}
