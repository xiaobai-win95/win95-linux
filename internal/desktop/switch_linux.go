//go:build linux

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (d *Desktop) openWeChat() {
	if _, err := os.Stat("/usr/bin/win95-wechat"); err != nil {
		d.MessageBox("微信", "当前镜像缺少微信启动组件（需要 v26.4 或更新）。", IconError, []string{"确定"}, nil)
		return
	}
	d.MessageBox("微信 WeChat",
		"微信会运行在 Win95 窗口内的 X11 嵌套代理中；需要完整桌面时，\n"+
			"可以双击桌面的“切换到 XFCE 桌面”，注销后返回 Win95。\n\n"+
			"首次使用需要联网安装以下内容：\n\n"+
			"· Arch Linux ARM glibc 子系统（约 800MB）\n"+
			"· Mesa 图形运行库（约 200MB）\n"+
			"· 微信官方 ARM64 安装包（约 200MB）\n"+
			"· X11 代理组件（Xvfb 等，首次也会自动安装）\n\n"+
			"至少需要 5GB 可用磁盘空间。安装期间 Win95 桌面仍可使用，\n"+
			"进度会显示在“安装进度”窗口中，完成后微信会出现在 X11 代理窗口里。",
		IconQuestion, []string{"继续", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if _, err := os.Stat("/opt/archlinux/opt/wechat/.win95-ok"); err != nil {
				d.openInstallStatus("微信安装")
			}
			d.openX11Proxy("win95-wechat")
			d.Notify("微信", "将在 X11 嵌套代理中安装并启动；完整桌面可用“切换到 XFCE 桌面”。")
		})
}

func (d *Desktop) installArch() {
	if _, err := os.Stat("/opt/archlinux/usr/bin/pacman"); err == nil {
		d.MessageBox("Arch Linux ARM (glibc)",
			"Arch Linux ARM 已安装在 /opt/archlinux。\n"+
				"pacman 使用清华 TUNA 镜像。\n\n"+
				"在终端中运行：\n"+
				"  pacman -Syu\n"+
				"  pacman -S 软件包名\n"+
				"  win95-arch-run bash\n\n"+
				"如果 pacman 报证书错误或数据库缺失，请点“检查并修复”。",
			IconInfo, []string{"确定", "检查并修复"}, func(i int) {
				if i == 1 {
					d.repairArch()
				}
			})
		return
	}
	d.MessageBox("Arch Linux ARM (glibc)",
		"将下载 Arch Linux ARM（glibc，约 800MB）到 /opt/archlinux，\n"+
			"并提供真实的 pacman，软件源为清华 TUNA 镜像。\n\n"+
			"安装期间桌面仍可使用，进度会显示在“安装进度”窗口中。\n"+
			"需要约 3.5GB 可用磁盘空间。继续吗？",
		IconQuestion, []string{"继续", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			d.openInstallStatus("Arch Linux 安装")
			cmd := exec.Command("/usr/bin/win95-arch-bootstrap")
			cmd.Dir = "/"
			cmd.Env = []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"HOME=/root",
				"WIN95_INSTALL_TITLE=Arch Linux 安装",
			}
			if err := cmd.Start(); err != nil {
				d.MessageBox("Arch Linux", "无法启动安装任务：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			_ = cmd.Process.Release()
			d.Notify("Arch Linux", "正在安装 glibc 子系统，进度见“安装进度”窗口。")
		})
}

// repairArch fixes a broken pacman database or CA trust store in an existing
// chroot without reinstalling it.
func (d *Desktop) repairArch() {
	if _, err := os.Stat("/usr/bin/win95-arch-repair"); err != nil {
		d.MessageBox("Arch Linux", "当前镜像缺少修复组件（需要 v26.4 或更新）。", IconError, []string{"确定"}, nil)
		return
	}
	d.openInstallStatus("Arch Linux 修复")
	cmd := exec.Command("/usr/bin/win95-arch-repair")
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"WIN95_INSTALL_TITLE=Arch Linux 修复",
	}
	if err := cmd.Start(); err != nil {
		d.MessageBox("Arch Linux", "无法启动修复任务：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	_ = cmd.Process.Release()
	d.Notify("Arch Linux", "正在检查并修复 pacman 数据库与证书。")
}

func (d *Desktop) launchShortcut(execLine string, terminal bool) {
	execLine = strings.TrimSpace(execLine)
	if execLine == "" {
		d.MessageBox("快捷方式", "这个快捷方式没有可执行的命令。", IconError, []string{"确定"}, nil)
		return
	}
	if terminal {
		title := "终端 - 快捷方式"
		if fields := strings.Fields(execLine); len(fields) > 0 {
			title = "终端 - " + filepath.Base(fields[0])
		}
		d.openTerminalCommand(title, []string{"/bin/sh", "-c", "exec " + execLine}, "/root", "")
		return
	}
	proxy := d.x11ProxyWindow("", nil)
	proxy.launchShell(execLine, terminal)
}

// switchToXFCE hands the display to a full Xorg/XFCE session. The switch
// script runs inside a real terminal so its apk prompt and progress output
// stay visible and interactive.
func (d *Desktop) switchToXFCE() {
	if _, err := os.Stat("/usr/bin/switch-to-xfce"); err != nil {
		d.MessageBox("切换到 XFCE 桌面",
			"当前镜像缺少桌面切换组件（需要 v26.4 或更新）。", IconError, []string{"确定"}, nil)
		return
	}
	d.MessageBox("切换到 XFCE 桌面",
		"将关闭 Windows 95 桌面并启动完整的 XFCE 桌面会话。\n\n"+
			"· 首次使用若尚未安装 Xorg/XFCE，切换脚本会在终端里询问并安装。\n"+
			"· 在 XFCE 中注销后会自动返回 Windows 95 桌面。\n"+
			"· 微信、Firefox 等程序也可以在 XFCE 中直接使用。\n\n"+
			"现在切换吗？",
		IconQuestion, []string{"切换", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			d.openTerminalCommand("XFCE 桌面切换", []string{"/bin/sh", "/usr/bin/switch-to-xfce"}, "/root", "xfce-switch")
			d.Notify("XFCE", "正在切换到完整 XFCE 桌面，注销后返回 Win95。")
		})
}

// launchXFCEApp keeps the old helper name for callers that only need one X11
// application inside the nested proxy.
func (d *Desktop) launchXFCEApp(app string) {
	d.openX11Proxy(app)
}
