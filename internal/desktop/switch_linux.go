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
