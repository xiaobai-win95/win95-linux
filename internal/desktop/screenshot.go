package desktop

// takeScreenshot asks the host to encode the current framebuffer frame and
// then reports the saved file in a balloon.
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
	d.Notify("截图已保存", path)
}
