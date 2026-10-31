//go:build linux

package desktop

import (
	"os"
	"os/exec"
	"strings"
)

func (d *Desktop) switchToXFCE() {
	_ = os.Remove(launchRequestPath)
	d.launchXFCE()
}

func (d *Desktop) launchShortcut(execLine string, terminal bool) {
	execLine = strings.TrimSpace(execLine)
	if execLine == "" {
		d.MessageBox("快捷方式", "这个快捷方式没有可执行的命令。", IconError, []string{"确定"}, nil)
		return
	}
	request := execLine
	what := "这个程序是 X11 图形程序"
	if terminal {
		// XFCE's autostart does not honor the Terminal key, so give the
		// command a terminal window explicitly.
		request = "xfce4-terminal -x " + execLine
		what = "这个程序需要在终端窗口中运行"
	}
	d.MessageBox("在 XFCE 中运行", what+"，不能在 Win95 桌面上直接显示。\n\n现在切换到 XFCE 桌面并启动它吗？", IconQuestion,
		[]string{"切换并启动", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if err := os.WriteFile(launchRequestPath, []byte(request+"\n"), 0o600); err != nil {
				d.MessageBox("快捷方式", "无法写入启动请求：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			d.launchXFCE()
		})
}

// launchXFCEApp starts one X11 program on tty2 while Win95 stays alive on
// tty1. Closing the program switches the console back automatically.
func (d *Desktop) launchXFCEApp(app string) {
	if _, err := os.Stat("/usr/bin/Xorg"); err != nil {
		d.MessageBox("XFCE 应用", "XFCE 图形环境尚未安装。\n\n请先在终端执行：\napk update && apk add xorg-server xinit xf86-input-libinput xf86-video-fbdev xfce4 xfce4-terminal firefox dbus dbus-x11 font-wqy-zenhei\n\n安装完成后，双击图标即可在后台启动。", IconInfo, []string{"确定"}, nil)
		return
	}
	exe, err := exec.LookPath(app)
	if err != nil {
		hint := "apk add " + app
		if app == "firefox" {
			hint = "apk add firefox"
		} else if app == "xfce4-terminal" {
			hint = "apk add xfce4-terminal"
		}
		d.MessageBox("XFCE 应用", "没有找到 "+app+"。\n\n请先在终端执行：\n  "+hint, IconInfo, []string{"确定"}, nil)
		return
	}
	if _, err := os.Stat("/usr/bin/win95-xfce-app"); err != nil {
		d.MessageBox("XFCE 应用", "系统缺少 win95-xfce-app 启动组件。\n\n请使用 v26.1 或更新的镜像。", IconError, []string{"确定"}, nil)
		return
	}
	cmd := exec.Command("/usr/bin/win95-xfce-app", exe)
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=zh_CN.UTF-8",
	}
	if err := cmd.Start(); err != nil {
		d.MessageBox("XFCE 应用", "无法启动 XFCE 应用：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	_ = cmd.Process.Release()
}

func (d *Desktop) launchXFCE() {
	for _, path := range []string{"/usr/bin/Xorg", "/usr/bin/startxfce4"} {
		if _, err := os.Stat(path); err != nil {
			_ = os.Remove(launchRequestPath)
			d.MessageBox("切换桌面", "XFCE 尚未安装。\n\n请先在终端执行 switch-to-xfce，按提示安装 Xorg/XFCE 后再切换。", IconInfo, []string{"确定"}, nil)
			return
		}
	}
	cmd := exec.Command("/usr/bin/switch-to-xfce")
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=zh_CN.UTF-8",
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(launchRequestPath)
		d.MessageBox("切换桌面", "无法启动 switch-to-xfce：\n"+err.Error(), IconError, []string{"确定"}, nil)
	}
}
