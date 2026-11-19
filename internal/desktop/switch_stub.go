//go:build !linux

package desktop

func (d *Desktop) launchShortcut(string, bool) {
	d.MessageBox("切换桌面", "X11 图形程序只能在 Linux 虚拟机上启动。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) launchXFCEApp(string) {
	d.MessageBox("切换桌面", "X11 图形程序只能在 Linux 虚拟机上启动。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) switchToXFCE() {
	d.MessageBox("切换桌面", "XFCE 桌面只能在 Linux 虚拟机上启动。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) openWeChat() {
	d.MessageBox("微信", "微信只能在 Linux 虚拟机上运行。", IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) installArch() {
	d.MessageBox("Arch Linux", "glibc 子系统只能在 Linux 虚拟机上安装。", IconInfo, []string{"确定"}, nil)
}
