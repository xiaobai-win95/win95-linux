# Windows 95 Linux 中文版 26.5（经典 Win95 外壳、AI 助手、极速搜索与 OTA 更新客户端） · UTM 配置与安装说明

## 一、交付文件

| 文件 | 说明 |
| --- | --- |
| `win95-installer-aarch64.img` | 可启动的 UEFI 安装盘镜像（raw GPT，130 MiB），系统版本 26.5，内置经典 Win95 外壳、AI 助手、OTA 更新客户端与 Office 365 启动器；**随 26.5 Release 上传** |
| `win95-installer-aarch64.img.sha256` | SHA-256 校验值 |
| `win95-ota-<版本>-aarch64.tar.gz` | 更新包，由构建脚本按 `VERSION` 生成；**从 26.4 起**随 Release 上传 |
| `win95-ota-<版本>-aarch64.tar.gz.sha256` | 更新包 SHA-256 校验值 |
| `latest.json` | 更新清单；**从 26.4 起**上传到 Release，客户端通过 `releases/latest/download/latest.json` 读取 |

校验方法：

```sh
cd /Users/madic/aiwork/win95linux/out
shasum -a 256 -c win95-installer-aarch64.img.sha256
```

镜像 SHA-256（随 v26.5 Release 上传）：

```
f20f6605e2c6a0fef1983aa28887dd2d8da98708713b0551c450646eb6272bfc
```

> 这是**磁盘镜像**，不是 ISO，不要挂到“CD/DVD”上。必须作为 UTM 的硬盘设备导入。

---

## 二、环境要求

- Apple Silicon Mac（ARM64），macOS 11 或更高版本。
- UTM（App Store 版或官网版均可），使用 **Apple Virtualization（虚拟化）** 后端。
- 安装器与已安装系统均为 **UEFI + VirtIO**，**不使用 PS/2**。
- 目标磁盘至少 1 GiB；只体验桌面建议 8 GiB。
- 如需安装微信：Arch 子系统、运行库与微信本体约占 5 GB，**建议 16 GiB 或更大**。
- 安装过程会**清空目标磁盘全部分区和数据**。

本机不需要安装 QEMU；UTM 使用 Apple Virtualization 框架运行。

---

## 三、创建安装用虚拟机

1. 打开 UTM，点击 **+** → **Virtualize（虚拟化）** → **Linux**。
2. 确认架构为 **ARM64 / aarch64**。不要选择“Emulate（仿真）”。
3. 在虚拟机设置中配置：
   - **System / 系统**
     - Memory：2048 MiB 以上，建议 4096 MiB。
     - CPU：2 核以上。
     - Boot：UEFI（默认）。
   - **显示 / Display**
     - Apple 虚拟化后端不提供 “Virtio GPU / 仅控制台” 之类的设备选项，显示设备由 UTM 自动配置。
     - 如果显示设置中有 **OpenGL** 选项，请开启；这样图形桌面会更流畅。
     - 分辨率可任选，系统支持各种分辨率；推荐 **1024 × 768** 或更高，桌面会按当前 framebuffer 尺寸绘制。
     - **HiDPI (Retina)** 可按需要开启；如果界面元素显得过小，关闭后重新启动虚拟机即可。
     - **动态** 分辨率可选；如果在运行中改变分辨率后画面没有及时刷新，注销并重新进入桌面。
   - **Input / 输入**
     - 键盘、鼠标使用 **USB**（默认）。
     - 不要添加 PS/2 设备；Apple Virtualization 本身也不提供 PS/2。
   - **Network / 网络**
     - 网络模式选择 **桥接 / Bridged**。
     - 桥接接口必须选 **Wi-Fi**（通常是 en0）；不要选 **Ethernet / 以太网**，否则虚拟机可能无法联网。
     - 系统会通过 DHCP 自动获取与 Mac 同一网段的地址。
4. 添加磁盘（“驱动器”）：
   - UTM 的 **“可移除驱动器”** 只表示镜像文件不放进 `.utm` 包，和 USB / PS/2 无关，勾选或不勾选都可以。
   - **Drive 1：安装盘**
     - 选择 **Import Drive / 导入驱动器**，选中 `win95-installer-aarch64.img`。
     - 勾选 **“只读?”**，避免安装过程中误写安装盘。
     - Apple Virtualization 只支持 raw 镜像；UTM 可能把镜像复制进 `.utm` 包，属正常现象。
   - **Drive 2：目标磁盘**
     - 点 **New Drive / 新建**，大小 **8 GiB 或更大**（10 GB 也可以）。
     - **不要勾选“使用 NVMe 磁盘接口”**；不勾选时使用 VirtIO 磁盘，系统内置驱动只支持这一种。
     - **“使用 Apple Sparse 映像格式”** 可保持勾选，节省宿主磁盘空间。
     - 创建后确认该盘的 **“只读?” 未勾选**。
   - 保持安装盘在前、目标盘在后。
   - 驱动器列表中的 **4 MB 映像** 是 UTM 的 EFI 变量存储，属正常项，不要删除。
5. 保存设置并启动虚拟机。

---

## 四、安装系统

1. 出现 systemd-boot 菜单后等待 3 秒，或选择 **Windows 95 Linux Installer**。
2. 进入 Win95 风格安装程序后：
   - 安装程序**不会预选任何磁盘**；请在“目标磁盘”列表中手动点选你刚新建的那块 **8–10 GiB 磁盘**（设备名可能是 `/dev/vda` 或 `/dev/vdb`，以容量为准）。
   - 如果选中的磁盘上已有分区表（例如放数据的 64 GB 磁盘），会先弹出 **“磁盘已有数据”** 警告；点击 **取消** 返回，重新选择正确的磁盘。
   - 130 MB 的安装盘可能以只读启动介质方式提供，因而不出现在列表中，属正常现象。
   - 红字提示的磁盘就是即将被清空的磁盘；确认无误后点击 **开始安装**，在确认框中再次核对设备名与容量。
3. 安装程序依次执行：写 GPT、格式化 EFI 分区（FAT32）、格式化共享超级分区（ext4，`WIN95SUPER`）、把系统解压进 `slots/a`、复制内核与引导程序。
4. 完成后屏幕显示“安装完成”和超级分区 PARTUUID。
5. 点击 **关闭计算机**。等待虚拟机完全停止。

> 不要在安装完成前强制关闭虚拟机。

---

## 五、首次启动已安装系统

1. 打开该虚拟机的设置，进入 **Drives / 驱动器**。
2. **移除安装盘**（`win95-installer-aarch64.img`），只保留目标磁盘。
3. 确认目标磁盘仍在第一位，保存设置并启动。
4. 出现 3 秒启动菜单后进入 **Windows 95 Linux 中文版**。
5. 等待系统初始化网络并启动图形桌面。首次启动可能需要几秒钟。
6. 首次进入桌面会弹出**欢迎界面与 Apache License 2.0 许可协议**，滚动阅读后点击"接受并继续"即可进入桌面；接受状态保存在 `/root/.config/win95/desktop.json`，之后不再弹出。拒绝则下次启动仍会显示。

如果仍然进入安装程序，说明安装盘没有移除，或启动顺序仍指向安装盘。

---

## 六、安装后的系统

- 图形桌面：Win95 风格任务栏、开始菜单、窗口、对话框。
- 程序菜单：收件箱、我的公文包、游戏、在线服务、Netscape Navigator 4.8、AI 助手、Wine 兼容层、记事本、计算器、日历、终端、Windows PowerShell、PowerShell ISE、文件管理器、网络浏览器、**Office 365 全件套**、**Office 组件管理**、微信 WeChat、纸牌、扫雷、方块世界 95、DOOM 95、三维弹球、画图、图片查看器、图形化下载器、软件包管理器、**系统更新（OTA）**、Arch Linux (glibc / pacman)、网络属性、连接监视器与防火墙、局域网快传、系统级 VNC 服务器、按键精灵、设置中心、屏幕键盘、视觉整活演示、任务管理器、注册表编辑器、**切换到 XFCE 桌面**，以及 X11 嵌套代理（XFCE 终端、Firefox、微信等 X11 程序都显示在 Win95 窗口里）。
- **X11 代理与完整 XFCE 并存**：X11 代理只负责把单个程序嵌进 Win95 窗口；桌面的“切换到 XFCE 桌面”仍会关闭 Win95 桌面、启动完整的 Xorg/XFCE 会话，注销后自动返回 Win95。两者互不替代。
- 设置中心包含语言、显示、字体、主题、时间、地区、关于与许可协议；界面支持简体中文、繁體中文、English、日本語、한국어、Français、Deutsch、Español、Русский、Português、Italiano 等，安装镜像自带全部语言。
- 显示属性：6 种桌面背景（经典青色、纯青色、海洋蓝、云彩、午夜星空、石板灰），并可配置飞线、星空或飞行窗口屏保与 1–120 分钟的等待时间；设置会保存到 `/root/.config/win95/desktop.json`。
- 运行与查找：开始菜单新增“运行”（支持程序别名、文件/文件夹路径、网址和最近命令历史）；“查找”（Ctrl+F）打开经典“查找：所有文件”对话框，可同时填写文件名称与包含文字，后台索引 + 全盘遍历，毫秒级返回，双击结果在文件管理器中定位。Ctrl+Space 或连续按两次 Alt 可呼出屏幕中央的聚光灯搜索框，↑/↓ 选择、Enter 打开。
- 回收站：可右键回收站图标或在回收站窗口中选择“清空回收站”，一次永久删除全部项目。
- 文件拖放：从文件管理器把文件拖到回收站图标/回收站窗口会删除文件；拖到桌面空白处会创建名为“原名 - 快捷方式”的快捷方式。
- 实用工具：Print Screen 截屏（PNG + BMP 保存到 `/root/桌面`，并自动在画图中打开）、图形化 Wget 下载器、任务栏网络状态指示灯、通知气泡。
- 任务栏系统托盘：实时显示 CPU 与内存占用率（读取 `/proc/stat`、`/proc/meminfo`，每秒刷新），并用绿/红/灰指示灯显示外网连通性。
- **任务管理器**（开始 → 程序 → 任务管理器）：解析 `/proc/[pid]/stat` 列出真实 PID、进程名、状态与常驻内存；顶部两条进度条分别显示 CPU 与内存总占用率；选中进程点“结束任务”会调用 `SIGKILL` 直接终止。
- **注册表编辑器**（开始 → 程序 → 注册表编辑器）：以树状结构编辑 `/etc/win95.ini` 中的桌面背景色、字体、主题、语言等键值；保存后当前桌面立即热更新，无需重启。
- 终端：BusyBox ash，支持伪终端、ANSI 颜色、滚动。
- 浏览器：支持 JavaScript（ECMAScript 5.1）、本地页面、HTTP/HTTPS、超链接跳转与文件下载；页面中的图片（含 data URI）、彩色按钮、输入框、密码框、下拉框、复选框可正常显示和交互；首次打开会自动安装本地运行时依赖包；若缺少系统 CA 证书会尝试通过 apk 联网补装（离线时仅提示，不影响本地页面）。下载保存到 `/root/下载`。mcjs.link 的 Minecraft 需要 WebGL2，会引导到 Firefox（Mesa llvmpipe 软件 WebGL2）。
- 文档目录：`/root/文档`、`/root/桌面`、`/root/下载`；画图保存的图片在 `/root/图片`。
- 右键菜单：桌面、桌面图标、窗口标题栏、任务栏（开始按钮/任务按钮/时钟/托盘/空白处）均可右键。
- 桌面文件操作：新建文件夹/文本文档、重命名、删除到回收站、查看属性、拖动排列、自动排列与排序、F5 刷新。
- 关机和睡眠：开始菜单底部提供 **注销**、**重新启动**、**关闭计算机**，以及 **睡眠**，均带确认框。
- 网络：按第三节配置为 **桥接 Wi-Fi**，系统通过 DHCP 获取局域网地址；可在终端用 `ifconfig`、`ping` 检查。
- 系统盘数据保存在目标 raw 磁盘镜像中，重启后保留。

### 26.5 新增功能

- **经典桌面图标**：我的电脑、网上邻居、收件箱、Internet Explorer、Netscape Navigator、回收站、我的公文包、游戏、在线服务、尝试 The Microsoft Network、AI 助手、切换到 XFCE 桌面、设置中心。开始菜单顶部是 AOL & Internet 免费试用、AOL 3.0i（含 IE 4.0）、AOL Canada 3.0i（含 IE 4.0）、新建 Office 文档、打开 Office 文档。
- **在线服务**：AOL / AOL Canada / MSN 使用经典登录框与 56.6 Kbps 模拟拨号进度，连接后显示频道（今日焦点、邮件、新闻、游戏、MSN Today 等）。全部为离线演示，不会拨号、联网或计费。连接后可一键打开收件箱、游戏或浏览器。
- **Netscape Navigator 4.8**：双击图标首次启动会询问是否安装，模拟安装约 2.4 秒后写入 `/root/.win95-netscape48`；之后直接打开 Netscape 品牌窗口，首页为 `about:netscape`。它与 Internet Explorer 各自使用独立窗口，互不抢占。可在“在线服务”中再次启动。
- **收件箱**：收件箱 / 已发送 / 已删除三个文件夹，支持预览、未读标记、删除与永久清除；工具栏“新邮件”在记事本中撰写。
- **我的公文包**：添加文件时复制到 `/root/公文包` 并在 `.origins.json` 记录来源；修改后点“同步”，较新的版本会写回原路径。适合在桌面与项目目录之间搬运文件。
- **极速搜索**：开始 → 查找（Ctrl+F）呼出经典“查找：所有文件”，可同时填写名称与包含文字。后台索引 + 全盘遍历，毫秒级返回；双击结果在文件管理器中定位。
- **聚光灯**：Ctrl+Space 或连续按两次 Alt，屏幕中央出现 Win95 输入框；输入后在后台搜索文件名与内容，↑/↓ 选择，Enter 用默认程序打开，Esc 关闭。
- **极客系统监视器与任务管理器**：系统监视器使用 Win95 折线/网格图显示 CPU、内存、网络上行/下行与磁盘 I/O；任务管理器提供 htop 风格进程树，右键可结束进程、限制 CPU（cgroup v2 `cpu.max`）与网络带宽（`tc tbf`）。
- **网络属性与防火墙**：开始 → 程序 → 网络属性，实时显示 IP、网关、DNS，一键 Ping 与 DNS 测速；连接监视器读取 `/proc/net` 列出 TCP/UDP 连接，右键可“切断连接”或“永久封禁该 IP”（iptables `WIN95-BAN`）。
- **局域网快传**：任务栏网络图标或桌面“网上邻居”，显示本机 IP 与共享页面（端口 8765–8784）。手机/Mac 用浏览器上传文件到 `/root/下载`；在 Win95 中选择文件可生成二维码供手机下载。
- **系统级 VNC 服务器**：开始 → 程序 → 系统级 VNC 服务器，纯 Go RFB 3.8 帧缓冲推流，Mac 的 VNC 客户端可直接控制整个桌面（鼠标、键盘与剪贴板）。
- **宿主机 Z: 盘**：我的电脑 → 添加 Z: 盘，输入 Mac 宿主机共享目录路径，即可在 Win95 中读写；路径会保存在桌面配置中。
- **截屏与画图**：Print Screen 截取全屏，保存 PNG 到 `/root/桌面` 并自动在画图中打开；新增选区与马赛克工具，可对截图打码后保存。
- **按键精灵**：程序 → 按键精灵，录制键盘/鼠标为 JSON 脚本（`/root/宏`），支持回放、循环次数与间隔；Esc 停止。
- **DOOM 95**：纯 Go 光线投射 3D 迷宫，带纹理墙面、地面与天花板，WASD + 鼠标转向，锁定 30 FPS，M 键切换小地图。
- **虚拟桌面与平铺**：Alt+1 / Alt+2 切换工作区；Alt+←/→ 左右半屏，Alt+↑/↓ 上下半屏，窗口随工作区自动调整。
- **AI 助手**：桌面“AI 助手”。命令助手把中文需求转成 Linux 命令：未配置密钥时使用本地规则（查看磁盘、内存、网络、查找、安装等）；在“设置”中选择通义千问或 DeepSeek 并填入 API Key 后调用真实 OpenAI 兼容接口。命令只会在点击“执行”后运行，`rm -rf`、`mkfs`、`dd of=/dev/…` 等危险命令会弹出二次确认。“赛博义体”标签页提供真实大模型聊天，可保存到记事本。API Key 保存在 `/root/.win95-ai.json`（0600）。
- **Wine 兼容层**：程序 → Wine 兼容层。首次运行需点击“安装 Wine”，从 Alpine community 安装 Wine 11（约数百 MB，需联网）。安装后可运行 `.exe` / `.msi`，C: 盘位于 `/root/.wine/drive_c`，winecfg 显示在 X11 嵌套代理中。本机是 ARM64：可运行 ARM64 Windows 程序；x86 程序还需要 Box64/Box86 或 Arch 子系统。

### 应用与软件安装

- **软件包管理器**（开始 → 程序 → 软件包管理器，或 设置 → 添加/删除软件）
  - 首次打开会自动执行 `apk update` 下载软件源索引；也可随时点 **刷新**（F5）。
  - 默认软件源是**清华大学 TUNA 镜像**（`mirrors.tuna.tsinghua.edu.cn`），国内下载更快；如果镜像同步延迟或不可用，可在终端切回官方源（见常见问题）。
  - 在搜索框输入名称或描述关键词，回车或点 **搜索**；选中条目后点 **安装(I)** / **卸载(U)**，双击条目也可直接安装/卸载。
  - 底部日志显示 apk 的下载和安装过程，完成后列表状态会更新。
  - 软件包管理器使用 **Alpine（musl）** 的 apk 与 Alpine 官方 ARM64 仓库。
  - 需要 glibc 或 Arch 软件时，使用开始菜单的 **Arch Linux (glibc / pacman)**：安装后可在终端运行 `pacman -Syu`、`pacman -S 包名`，软件源为清华 TUNA 的 Arch Linux ARM 镜像，与 apk 互不影响。
  - 软件源中的包名和描述都是英文，请用英文关键词搜索（如 `vim`、`htop`、`python3`、`curl`、`git`）；输入中文通常没有结果。
  - 安装成功后，程序会自动在 `/root/桌面` 生成快捷方式（读取包内的 `.desktop` 文件，带 `X-Win95-Shortcut=true` 标记）；卸载时只删除自动生成的快捷方式。命令行程序可用终端运行。
  - 依赖 X11 的图形程序会在 Win95 的“X11 嵌套代理”窗口里运行（Xvfb `:1` + XGetImage/XTest），不会切换控制台；终端程序（`Terminal=true`）同样通过代理里的 `xfce4-terminal` 打开。首次使用若缺少组件，会自动弹出安装进度。需要完整桌面时，双击桌面的“切换到 XFCE 桌面”，XFCE 与代理可以共存。
  - 终端中等价命令：`apk update`、`apk search 关键词`、`apk add 包名`、`apk del 包名`。
- **扫雷**：左键翻开方块，右键插旗；菜单“游戏”可切换初级 9×9、中级 16×16、高级 30×16，F2 重开一局。
- **纸牌**：经典 Klondike 接龙，保证每局可解（公平发牌算法，不再出现死局）。拖动牌移动，红黑交替、点数递减堆叠，空列只能放 K；点左上角牌堆发三张牌，双击顶牌自动收进接龙堆；支持 Ctrl+Z 撤销、F2 发新牌，状态栏显示得分、时间和步数。
- **画图**：铅笔、画刷、橡皮、直线、矩形、椭圆，底部调色板和“笔宽”可选；文件 → 打开（Ctrl+O）可载入 PNG/BMP，超大图片会自动缩小并居中；文件 → 保存（Ctrl+S）输出 24 位 BMP，默认目录 `/root/图片`。
- **图形化下载器**：开始 → 程序 → 图形化下载器，输入 `http://` 或 `https://` 地址（省略协议自动补 `https://`）；文件保存到 `/root/下载`，显示进度条、已下载大小和速度，可取消，完成后弹出气泡。下载时先写入 `.part`，完成后再改名，避免留下半截文件。
- **截屏**：按 Print Screen，或开始 → 设置 → 截屏；当前 framebuffer 画面保存为 `/root/桌面/截图-YYYYMMDD-HHMMSS.png` 与同名 `.bmp`（24 位 BMP），并自动在画图中打开 PNG。画图提供铅笔、画刷、选择与马赛克工具，可对截图打码后保存。请先点一下 UTM 窗口，确保按键送进虚拟机。
- **图片查看器**：开始 → 程序 → 图片查看器，或在文件管理器中双击 `.png` / `.bmp` 文件；支持 PNG 透明通道、滚轮缩放和拖动平移，截屏得到的两种格式都可直接打开。
- **Windows PowerShell**：开始 → 程序 → Windows PowerShell；支持常用 cmdlet、管道、Get-Help 等，未识别命令交给 `/bin/sh`。外部命令运行在真实伪终端上：输出实时显示、stdin 保持连接，`read`、`ssh`、包管理器确认提示等交互程序可以直接操作；`Ctrl+C` 中断当前命令（3 秒未退出则强制结束）。**默认没有 30 秒或任何运行时限**；需要限制时执行 `$PSCommandTimeout = 300`（秒），或在启动前设置环境变量 `WIN95_PS_TIMEOUT`。**PowerShell ISE** 提供脚本编辑区与输出区，F5 运行、Esc 停止、Ctrl+S 保存。
- **屏幕键盘**：`Ctrl+Alt+K` 或开始 → 程序 → 屏幕键盘；鼠标点击输入字母、数字、符号和控制键，再次按快捷键收起。
- **方块世界 95（真 3D）**：开始 → 程序 → 方块世界 95；默认即为第一人称 3D 视角，世界为 240×48×88 的体积体素（逐像素光线步进，真实的遮挡、透视、雾气与昼夜光照）。W/A/S/D 移动，空格跳跃，方向键或按住右键拖动视角，左键挖掘、右键放置，1–9 或滚轮选择方块；E 打开合成面板（木板、工作台、圆石、玻璃、砖块，部分配方需靠近工作台），V 切回 2D 剖面。目标环境 UTM Apple 虚拟化没有 GPU，游戏由 CPU 实时渲染；X11 代理中的 Firefox 等程序可使用 Mesa/llvmpipe 的软件 OpenGL。挖掘、放置、跳跃和合成有 PC 喇叭音效（UTM 可能无声）；自动存档到 `/root/文档/方块世界存档.json`。
- **视觉整活（仅演示）**：开始 → 程序 → 视觉整活：苏尔科…；先显示光敏性癫痫警告与确认框，可选择正常模式或温和模式（无闪烁、无反色）或取消。**确认后运行约 30 秒，中途不能退出**（无 Esc/中止按钮），但桌面输入仍然有效——点击开始按钮仍会弹出开始菜单（很快又被覆盖层盖住）。特效覆盖在真实桌面图层之上：先截图当前桌面制造移动动画，再依次播放六阶段”苏尔科”虚构病毒动画（桌面跳舞 → 绿色横纹 → 形状爆炸 → 中心宣告 → 吞噬漩涡 → 图标大乱斗 → 假蓝屏）；正常模式有**每秒一次的全屏反色**；运行中会弹出自动关闭的 DEAD 终端窗口和”你中了苏尔科病毒！”对话框。漩涡会吸入桌面图标、窗口和虚构的 `SULKO.SYS` 图标。动画只存在于内存，不调用 `mkfs`/`fdisk`/`dd`、不打开 `/dev`、不写入分区表、不删除或移动文件，结束后说明数据完好。**光敏性癫痫患者请选择温和模式或不要运行。**
- **通知气泡**：截图、下载完成、XFCE 下载完成和 XFCE 应用退出等事件会在任务栏右下角弹出黄色气泡，约 8 秒后自动消失。
- **PC 喇叭**：开机和错误对话框会尝试通过 `/dev/tty0`、`/dev/console` 响铃。UTM / Apple 虚拟化不一定暴露 PC 喇叭，没有声音属于正常现象。

### Arch Linux ARM（glibc）与微信

- **Arch Linux ARM 子系统**：开始 → 程序 → **Arch Linux (glibc / pacman)**。首次使用会下载约 800 MB 的 Arch Linux ARM 根文件系统到 `/opt/archlinux`，并配置 **清华 TUNA 镜像（唯一源）**；桌面保持可用，下载、解压和 pacman 初始化会显示在“安装进度”窗口中。下载优先使用 `curl`（首次会自动用 apk 安装；BusyBox wget 的 TLS 实现无法校验证书，部分镜像会直接返回 403），源依次为：清华 TUNA（HTTPS）→ TUNA（跳过证书校验）→ TUNA（HTTP）→ 中科大 → 北外 → 美国 FL 镜像（`fl.us.mirror.archlinuxarm.org`）→ Arch Linux ARM 官方。所有失败都会记录 HTTP 状态和错误原因。
- **真实 pacman**：在终端直接运行：
  ```sh
  pacman -Syu            # 同步并升级
  pacman -S 包名          # 安装软件（下载自清华镜像）
  win95-arch-run bash    # 进入 glibc 子系统的 shell
  ```
  pacman 运行在 chroot 中，与 Alpine 的 apk 互不干扰；首次安装会初始化密钥环，若镜像签名校验失败会自动退回到跳过签名校验的方式（配置写入 `/opt/archlinux/etc/pacman.conf`，日志在 `/var/log/win95-arch-bootstrap.log`）。
- **证书或 pacman 数据库损坏时**：开始 → 程序 → **Arch Linux (glibc / pacman)** → **检查并修复**，或在终端运行：
  ```sh
  win95-arch-repair          # 修复证书、pacman 数据库并只保留 TUNA 源
  win95-arch-repair --check  # 只检查，不修改
  ```
  修复工具会：创建 `/var/lib/pacman/local` 与 `/var/lib/pacman/sync`；从缓存的 `ArchLinuxARM-aarch64-latest.tar.gz` 恢复被删掉的 local 数据库（压缩包不在时重新下载并解压基础系统）；当 `/etc/ssl/certs/ca-certificates.crt` 缺失或被 `update-ca-trust` 清空时，复制 Alpine 自带的 Mozilla 根证书包；写入带 `/` 根挂载项的 `/etc/mtab`（否则 pacman 提交事务时报 `could not determine filesystem mount points` 或 `could not determine cachedir mount point`）；HTTPS 因证书问题失败时自动把 mirrorlist 切换到 `http://mirrors.tuna.tsinghua.edu.cn/archlinuxarm/...`。日志在 `/var/log/win95-arch-repair.log`。
- **微信 WeChat**：开始 → 程序 → **微信 WeChat**。首次运行会按顺序自动准备：X11 运行库 + Mesa 图形组件 → Arch glibc 子系统 → 官方 ARM64 微信安装包（约 200 MB，来自 `dldir1v6.qq.com`，失败时自动换用 `dldir1.qq.com`）→ 用 pacman 从清华镜像安装运行库，然后在 Win95 的 X11 嵌套代理窗口中启动微信（`DISPLAY=:1`），不会切换控制台。首次总下载量约 1.5 GB，需要约 5 GB 空闲磁盘和稳定网络；“安装进度”窗口会显示下载字节数、pacman 的 `(n/m)` 安装进度和阶段日志，关闭窗口不会中断安装。日志在 `/var/log/win95-wechat-setup.log`。微信以 `--no-sandbox` 运行在 root 用户下（Chromium/CEF 应用在 root 下的常规做法）。
  - **安装包路径**：`/var/cache/win95/WeChatLinux_arm64.deb` 在 Alpine 侧，chroot 里看不到它。安装器会先复制到 `/opt/archlinux/tmp/win95-wechat.deb`，再在 chroot 内用 `bsdtar` 按 `dpkg -x` 的方式解压到 `/opt/wechat`。
  - **libandromeda.so 报错的手动修复**：如果你自己解压后运行 `./wechat` 出现 `error while loading shared libraries: libandromeda.so`，说明动态库搜索路径没有指向解压目录，或内核拒绝了可执行栈标记。按下面的顺序在 chroot 内修复（先运行 `win95-arch-mounts`，否则 X11 和 DBus 也连不上）：
    ```sh
    # 宿主机（Alpine）侧：把 deb 复制进 Arch 子系统
    mkdir -p /opt/archlinux/tmp
    cp /var/cache/win95/WeChatLinux_arm64.deb /opt/archlinux/tmp/win95-wechat.deb
    win95-arch-mounts
    chroot /opt/archlinux /bin/bash

    # Arch chroot 内：安装编译/解压/运行库
    pacman -Syy --needed --noconfirm patchelf libarchive xz zstd \
      gtk3 glib2 nss nspr alsa-lib libpulse libsecret libcups \
      ttf-dejavu noto-fonts-cjk \
      libx11 libxcb libxext libxrender libxi libxfixes libxcursor \
      libxrandr libxinerama libxcomposite libxdamage libsm libice \
      libxtst libxss libxkbcommon libxkbcommon-x11 libglvnd mesa libdrm \
      at-spi2-core pango cairo gdk-pixbuf2 harfbuzz libnotify libepoxy \
      libpng libjpeg-turbo libwebp libtiff expat libffi pcre2 \
      xcb-util-cursor xcb-util-image xcb-util-keysyms xcb-util-renderutil xcb-util-wm \
      qt5-base qt5-svg qt5-x11extras qt5-declarative \
      xorg-xauth xorg-xhost xorg-xrandr xorg-xrdb xdg-utils fontconfig freetype2

    # 解压 deb（等价 dpkg -x）
    cd /tmp
    member=$(bsdtar -tf win95-wechat.deb | grep -E 'data\.tar(\.(xz|gz|zst|bz2))?$' | head -n1)
    rm -rf /opt/wechat
    bsdtar -xOf win95-wechat.deb "$member" | bsdtar -xf - -C /
    ln -snf /opt/wechat/wechat /usr/bin/wechat

    # 修正 RPATH 与可执行栈标记，把嵌套库链接到主程序旁
    cd /opt/wechat
    rpath='$ORIGIN:$ORIGIN/lib:$ORIGIN/lib64:/opt/wechat:/opt/wechat/lib:/opt/wechat/lib64'
    find . -mindepth 2 -type f -name '*.so*' | while IFS= read -r f; do
      b=$(basename "$f"); [ -e "./$b" ] || ln -sfn "$f" "./$b"
    done
    find . -type f \( -name '*.so' -o -name '*.so.*' -o -perm -u+x \) | while IFS= read -r f; do
      patchelf --clear-execstack "$f" 2>/dev/null
      patchelf --set-rpath "$rpath" "$f" 2>/dev/null
    done
    echo /opt/wechat > /etc/ld.so.conf.d/wechat.conf
    ldconfig
    ldd /opt/wechat/wechat | grep 'not found' || echo "all libraries resolved"
    ```
    修复后不要直接在没有显示环境的 chroot 里运行；从 Win95 桌面的“微信 WeChat”启动，或确认 `DISPLAY=:1`、`XDG_RUNTIME_DIR=/run/user/0`、`DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/0/bus` 后再执行 `/opt/wechat/wechat`。`/tmp/.X11-unix` 由 `win95-arch-mounts` 绑定进 chroot，手工挂载时容易漏掉。
  - 常见的隐藏依赖：`nss`、`nspr`、`alsa-lib`、`libxss`、`libxtst`、`libxkbcommon-x11`、`xcb-util-*`、`qt5-base`、`qt5-svg`、`qt5-x11extras`、`libsecret`、`libcups`、`at-spi2-core`、`mesa`。上面的 pacman 命令已经全部包含。
- **随附脚本**：`alpine-install-xfce4.sh` 已放进 `/root/桌面/`（Alpine 上的 XFCE4 安装脚本，联网执行 `setup-desktop` 并下载中文字体）。
- **注意**：Arch 子系统与微信不随安装镜像打包，需要联网下载；删除 `/opt/archlinux` 可以回收空间，下次使用时会重新安装。
- **关于挂载**：旧测试镜像曾在 `/opt/archlinux/etc/resolv.conf` 上做**文件级** `mount --bind`。`mount` 输出会把该文件所在的整个根文件系统（例如 `/dev/vda2`）显示在源列，看起来像把整块磁盘挂到了一个文件上；实际只是这一个文件。26.3 起改为直接复制 `/etc/resolv.conf`，不再产生该条目；旧系统执行一次 `win95-arch-umount` 或重启即可清掉。chroot 的 `/dev`、`/proc`、`/sys`、`/run` 仍按需挂载，并在初始化或微信安装完成后由 `win95-arch-umount` 释放。

### 系统托盘、屏保与剪贴板


- **系统托盘**：任务栏右下角显示 `CPU` 和 `RAM` 百分比与迷你条形图，每秒采样一次；左侧网络指示灯绿色表示外网可达，红色表示断网，灰色表示检测中，每 12 秒刷新一次；右键托盘选择“网络状态”可查看详情与 UTM 网络设置建议。
- **屏保与显示属性**：右键桌面 → 属性，或开始 → 设置 → 显示属性，可打开 Win95 风格的两页对话框。
  - **背景**：选择 6 种背景之一，右侧显示器会实时预览；点击“确定/应用”后立即应用并保存。
  - **屏幕保护程序**：可选“无”“变幻线”（Mystify）、“星空”或“飞行窗口”，等待时间 1–120 分钟；点击“预览”会全屏显示，移动鼠标或按任意键返回。默认 3 分钟、变幻线。
  - **飞行窗口**：满屏微小的 Win95 窗口从屏幕中心向四周飞散，越飞越大直到飞出屏幕，循环重生。
  - 屏保期间不接收应用输入，系统继续运行，也不会锁屏；设置保存在 `/root/.config/win95/desktop.json`。
- **剪贴板**：
  - 记事本支持鼠标拖动选择，`Ctrl+C` / `Ctrl+X` / `Ctrl+V` 复制、剪切、粘贴，`Ctrl+A` 全选。
  - 终端用 `Ctrl+Shift+C` 复制、`Ctrl+Shift+V` 粘贴（普通 `Ctrl+C` 仍发送中断信号）。
  - Win95 与 XFCE 共用 `/run/win95-clipboard`；XFCE 里的 `win95-clipd` 守护进程每 0.5 秒同步 X11 剪贴板，双向可用。切换桌面脚本会在注销前再抓取一次 X11 剪贴板，避免最后时刻的复制丢失。

### 右键菜单与桌面操作

- **桌面右键**：排列图标（自动排列、对齐到网格、按名称、按类型）、刷新（F5）、新建（文件夹 / 文本文档）、属性（打开显示属性对话框）。
- **图标右键**：打开、资源管理器、重命名、删除（Del）、属性。“我的电脑”等系统图标会禁用重命名和删除；回收站图标另有“清空回收站”，一次永久删除其中全部项目。
- **窗口右键**：在标题栏上右键，或按 `Alt+Space`，打开窗口系统菜单：还原、移动（灰色）、大小（灰色）、最小化、最大化、关闭（Alt+F4）。
- **任务栏右键**：
  - 开始按钮：打开开始菜单、资源管理器、关于本机；
  - 任务按钮：还原、最小化、最大化、关闭；
  - 时钟：查看日期和时间、系统监视器、关于本机；
  - 托盘：系统监视器、桌面属性、关于本机；
  - 空白处：层叠窗口、横向平铺、纵向平铺、最小化所有窗口、桌面属性。
- 菜单支持鼠标悬停展开子菜单，也支持键盘：方向键选择、Enter 执行、Esc 关闭；右键菜单打开时按其他键不会传给应用。
- **桌面即文件夹**：`/root/桌面` 中的所有文件和文件夹都会显示为图标，双击文件夹用文件管理器打开；文本类文件用记事本打开，HTML 用浏览器打开。
- **文件管理器**（开始 → 程序 → 文件管理器）：默认打开“我的电脑”，列出系统盘、我的文档/下载/桌面、Arch Linux 子系统、检测到的 UTM 共享文件夹与常用程序入口。
  - **标签页**：Ctrl+T 新建标签页、Ctrl+W 关闭当前标签页、Ctrl+Tab 切换标签页；顶部标签栏可点击切换，标签上的 `×` 关闭，`+` 新建。
  - **在此处打开终端**：工具栏的“终端”按钮或右键菜单“在此处打开终端”，会在当前目录启动一个 Win95 终端；进入 Arch 子系统后按钮变为“在此处打开 Arch 终端”，执行 `win95-arch-run bash` 进入 glibc chroot。
  - **创建快捷方式**：右键文件或文件夹 → “创建快捷方式”，在当前目录生成符号链接，名称为“原名 - 快捷方式.扩展名”，重名自动加编号；双击快捷方式打开原目标。
  - **显示隐藏文件**：按 Ctrl+H 或查看菜单 → “显示隐藏文件”，列出以 `.` 开头的文件与目录；再次切换即隐藏。
  - **更多文件类型**：文本/源码/配置（.txt/.md/.go/.c/.py/.json/.yaml/.toml/.ini…）用记事本打开；HTML 用浏览器；PNG/BMP/JPG/JPEG/GIF 用图片查看器；`.sh`/`.bash` 在终端中执行；压缩包（.zip/.tar/.gz/.xz/.7z…）在终端里列出内容；`.desktop` 快捷方式解析后启动；Word/Excel/PowerPoint/Draw/Math/Base/PDF 等办公文档交给 **Office 365 全件套**（缺少组件时先提示安装）；`.exe`/`.msi`/`.bat`/`.cmd` 交给 **Wine 兼容层**（未安装时先提示安装）；音视频、字体和磁盘镜像会给出对应说明或交给合适的程序。
  - **UTM 共享文件夹**：启动时读取 `/proc/self/mounts`，自动把 `virtiofs` / `9p` 挂载点（如 UTM 的共享目录）显示为“共享文件夹 - 名称”，双击即可浏览。
  - **进入子系统**：在“我的电脑”中双击“Arch Linux 子系统”直接浏览 `/opt/archlinux`；右键“Arch 子系统终端”或在 Arch 目录内点终端按钮进入 chroot。
- **文件操作**：在文件管理器中右键可新建文件夹、重命名（F2，带输入框和非法字符校验）、删除（移入 `/tmp/.trash` 回收站）、查看属性（路径、类型、大小、修改时间）；在回收站里再次删除才是永久删除。系统根目录、`/root` 等关键路径受保护，不能删除或重命名。
- **文件关联**：双击 `.sh` 在交互式终端中执行（不再另弹结果窗口），`.bash` 用 `/bin/bash` 执行，`.txt`/`.md` 等用记事本打开，`.png`/`.bmp`/`.jpg`/`.gif` 用图片查看器打开，`.html` 用浏览器打开，`.desktop` 按其中命令启动，`.exe`/`.msi`/`.bat`/`.cmd` 通过 Wine 打开，压缩包在终端中查看。
- **拖放**：在文件管理器中按住文件拖动，可放入回收站图标或回收站窗口（删除），也可拖到桌面空白处创建快捷方式。拖动时会显示半透明的文件图标，并提示“松开鼠标放入回收站”或“松开鼠标在桌面创建快捷方式”；按 Esc 取消拖动。虚拟入口（系统盘、我的文档、Arch 子系统、共享文件夹等）不会被误拖进回收站。
- **快捷方式命名**：拖到桌面的文件会以“原名 - 快捷方式.扩展名”命名，重名时自动加 `(2)`、`(3)`；双击快捷方式会打开原文件或文件夹。
- **排列图标**：关闭“自动排列”后可拖动图标，位置按屏幕边界和任务栏自动限制；右键可“对齐到网格”，或按名称/类型排序。重新开启自动排列会回到网格布局。
- **桌面快捷键**：方向键选择图标、Enter 打开、F2 重命名、Delete 删除、Ctrl+N 新建文件夹、F5 刷新、Esc 取消选择。
- **窗口操作**：双击标题栏在最大化与还原之间切换；拖动标题栏移动，拖动右下角调整大小；任务栏菜单可一键层叠、平铺或最小化所有窗口。
- 桌面每 3 秒自动扫描一次 `/root/桌面` 的变化；右键菜单打开或正在拖动图标时会延后刷新，避免菜单或图标位置被打断。

### 睡眠

- 打开 **开始 → 睡眠(S)…**，在确认框中选择 **睡眠**。系统会先绘制深蓝色睡眠画面，再尝试让机器进入睡眠。
- 实现方式：读取 `/sys/power/state`，优先写入 `freeze`（suspend-to-idle），失败时再尝试 `mem`（挂起到内存）。
- 唤醒：按任意键或移动鼠标。请先点一下 UTM 窗口让它获得焦点，否则键盘/鼠标事件不会送进虚拟机。唤醒后会弹出“系统已从睡眠中唤醒”。
- 是否真正暂停虚拟机取决于内核配置和 UTM / Apple 虚拟化的电源管理支持。若内核没有 `CONFIG_SUSPEND`、`/sys/power/state` 不可写或虚拟机拒绝睡眠，系统会弹出“无法进入睡眠”及具体原因，并留在桌面；不会丢失正在编辑的文件。
- 安装程序运行期间不能进入睡眠；睡眠与屏保是两个独立功能，屏保不会让系统睡眠。
- 如果 UTM 在睡眠后无法唤醒，请强制停止虚拟机再启动；这属于虚拟机电源管理行为，不影响磁盘上的系统。

### 关机、重启与注销

- 开始菜单底部：**注销(L)…**、**重新启动(R)…**、**关闭计算机(U)…**，都会弹出 Win95 风格的确认框。
- 关闭计算机调用 BusyBox `poweroff`，重新启动调用 `reboot`；GUI 不是 PID 1，因此不使用会返回 EPERM 的 `reboot(2)` 系统调用。
- 注销会让 `win95gui` 以退出码 42 结束，`startgui` 显示英文注销提示，按回车重新进入 Win95 桌面。
- **彩蛋**：“开始 → 设置 → 危险操作…”会显示全屏经典蓝屏（伪异常码、寄存器与栈内容），约 1 秒后按任意键返回；不会损坏系统或文件。
- **死手系统**：开始菜单中的红色“死手系统（核反击）…”会要求输入四位授权密码 `0000`，随后显示 10 秒发射倒计时；点“取消发射”可中止。若不取消，屏幕会先闪红约 1.6 秒，再显示蓝屏 `System Override: Nuclear Strike Confirmed.`，按任意键即可返回桌面。全程只是模拟，不会关机或删除数据。

### 多控制台（TTY）切换

- `Ctrl+Alt+F1`：Win95 图形桌面（tty1）。
- `Ctrl+Alt+F2` / `Ctrl+Alt+F3`：文本控制台 tty2 / tty3。首次切入时屏幕显示 `Please press Enter to activate this console`，按一次回车即进入 root shell；输入 `exit` 后控制台回到等待状态。
- 也可以在终端里执行 `chvt 2`、`chvt 1` 切换，`chvt 2` 不会再卡死。
- 切换到其他控制台时，`win95gui` 会收到内核的 VT 释放信号，主动关闭 `/dev/fb0`、停止绘制并丢弃输入，然后把显示让给文本控制台；切回 tty1 时重新打开 framebuffer 并恢复桌面。
- 旧镜像里 `chvt 2` 卡死的原因：VT 处于 `KD_GRAPHICS + VT_AUTO` 时，内核在 `change_console()` 里直接忽略切换请求，`chvt` 一直阻塞在 `VT_WAITACTIVE`。现在 GUI 用 `VT_PROCESS` 与内核握手（释放信号 SIGUSR1、获取信号 SIGUSR2），收到释放请求后关闭 framebuffer 并回 `VT_RELDISP`，切换由内核正常完成。
- 如果图形界面是在其他控制台处于前台时被 respawn 的，它会检测到 tty1 不在前台并保持暂停，不会覆盖别的控制台画面；切回 tty1 时自动恢复。

### X11 嵌套代理（X11-in-Win95）

X11 程序可以直接在 Win95 桌面窗口里运行，不必切换控制台；需要完整桌面时仍可切到 Xorg/XFCE。两者是并存关系，代理不会取代 XFCE：

- **虚拟显示**：代理在后台静默启动 `Xvfb :1 -screen 0 1024x768x24 -nolisten tcp -ac`，只提供一块 1024×768 的虚拟屏幕，不接管 tty1 的 framebuffer。
- **画面捕获**：Go 协程每隔约 110 ms 通过 `XGetImage` 抓取 `:1` 的像素，转换颜色后绘制到“X11 嵌套代理”窗口的客户区；内容未变化时跳过重绘。
- **输入回送**：窗口里的鼠标移动、左/右键、滚轮和键盘事件通过 XTest 扩展注入 `:1`，因此 `xfce4-terminal`、Firefox、微信等程序可以正常操作。右键会补发一次 button 3 的按下/释放。
- **入口**：桌面图标“XFCE 终端”“Firefox”“X11 代理”，开始 → 程序 → 对应条目，开始 → 运行（如 `firefox`、`xfce4-terminal`），浏览器的 mcjs.link 跳转，以及软件包管理器生成的图形快捷方式，全部走同一个代理窗口；重复打开会复用已有代理窗口。
- **按需安装**：首次使用若镜像里缺少 `Xvfb`、`firefox`、`xfce4-terminal` 等组件，会弹出确认并运行 `/usr/bin/win95-x11-setup`（Alpine 源，约数百 MB）；“安装进度”窗口显示阶段与日志，完成后自动启动刚才请求的程序。日志：`/var/log/win95-x11-setup.log`、`/tmp/win95-xvfb.log`、`/tmp/win95-x11-app.log`。
- **窗口管理**：Xvfb 里运行轻量窗口管理器 JWM（配置文件 `/root/.jwmrc`），窗口可移动、缩放、最小化，LibreOffice 的文件对话框与提示框能正常获得焦点。打开 Office 文档时代理会自动最大化，以利用 1024×768 的虚拟屏幕；输入法框与弹窗由 JWM 管理。
- **重连**：如果 Xvfb 异常退出，代理工具栏的“重连”会清理失效的 `/tmp/.X1-lock` 后重新启动虚拟显示。
- **完整 XFCE**：双击桌面的“切换到 XFCE 桌面”（或开始 → 程序 → 切换到 XFCE 桌面）会关闭 Win95 桌面，交给 tty1 上的 Xorg/XFCE；在 XFCE 中注销后自动回到 Win95。X11 代理在 Win95 内继续作为单程序窗口使用，不需要为它放弃完整桌面。旧的 `switch-to-xfce` / `win95-xfce-app` 脚本仍在磁盘上，切换入口现在重新指向完整的 XFCE 会话。

### Office 365 全件套（按需安装）

Office 组件不随系统镜像打包，首次使用时从 Alpine 软件源下载；安装完成后可以随时补装缺少的组件。

- **入口**：桌面“Office 365”图标，开始 → 程序 → **Office 365 全件套**，或开始 → 程序 → **Office 组件管理**。启动器以卡片列出 Word、Excel、PowerPoint、Draw、Math、Base、PDF、邮件与笔记；未安装的卡片点击后会直接打开组件管理并预选所需组件。
- **组件与下载量（约）**：
  | 组件 | 内容 | 大小 |
  | --- | --- | --- |
  | 核心引擎与中文字体（必装） | libreoffice-common/gtk、Noto CJK、文泉驿字体、Xvfb、DBus、JWM | 246 MB |
  | Word / Excel / PowerPoint / Draw / Math | libreoffice-writer / calc / impress / draw / math | 1–10 MB |
  | PDF 阅读与工具 | zathura、zathura-pdf-poppler、poppler-utils、qpdf | 6 MB |
  | 中文输入法 | fcitx5、拼音、GTK/QT 模块与配置工具 | 13 MB |
  | 界面与拼写语言包 | 简繁中文、英、日、韩、法、德、西、俄、葡、意 | 13 MB |
  | Base 数据库 | libreoffice-base + OpenJDK 17 运行时 | 65 MB |
  | 邮件 / 笔记 / OCR / 云盘 | Thunderbird / Zim / Tesseract / rclone | 4–95 MB |
  默认勾选核心、Writer、Calc、Impress、Draw、Math、PDF、中文输入法与语言包；Base、邮件、笔记、OCR、云盘同步默认不勾选。
- **安装流程**：组件管理窗口点“安装所选”，确认后运行 `/usr/bin/win95-office-setup`。脚本先 `apk update`，再按每 6 个包一组执行 `apk add --no-progress`，逐包校验是否安装成功，最后刷新字体缓存并写入 fcitx5 拼音配置。安装进度显示在“安装进度”窗口，桌面可继续使用；同时只允许一个安装任务（锁目录 `/run/win95-office-setup.lock`）。
- **补装**：组件管理窗口每 2 秒重新读取 `/lib/apk/db/installed`，已安装组件显示为“已安装”并保持勾选、不可取消；其余组件可继续勾选补装，只会下载缺少的软件包。
- **文件关联**：在文件管理器或桌面双击 `.doc/.docx/.docm/.dot/.dotx/.odt/.ott/.rtf/.wps`、`.xls/.xlsx/.xlsm/.xlt/.xltx/.ods/.ots/.csv/.tsv`、`.ppt/.pptx/.pptm/.pps/.ppsx/.pot/.potx/.odp/.otp`、`.odg/.otg/.vsd/.vsdx`、`.odf/.odc`、`.odb`、`.pdf` 时自动选择对应应用；组件未安装时先打开组件管理并预选，安装完成后继续打开该文档。
- **运行方式**：LibreOffice 通过 X11 嵌套代理启动（`SAL_USE_VCLPLUGIN=gtk3`，`--writer/--calc/--impress/--draw/--math/--base`），代理窗口自动最大化。后台是 1024×768 的 Xvfb :1、JWM 窗口管理器、私有会话总线 `/tmp/win95-x11-bus` 与 fcitx5 拼音；安装输入法后无需重启桌面。
- **日志**：`/var/log/win95-office-setup.log`（安装）、`/tmp/win95-fcitx5.log`（输入法）、`/tmp/win95-jwm.log`（窗口管理器）。
- **更大画面**：需要更宽的编辑区时，可切换到完整 XFCE 桌面，或在 UTM 虚拟机设置中调大显示分辨率；Win95 桌面的 X11 代理仍为 1024×768。

### 完整 XFCE 桌面切换

- **入口**：桌面图标“切换到 XFCE 桌面”、开始 → 程序 → “切换到 XFCE 桌面”，或终端执行 `/usr/bin/switch-to-xfce`。
- **流程**：切换脚本先停止 `win95gui`，把控制权交给 `startgui`；`startgui` 在 tty1 上启动 udev、Xorg 和 `startxfce4`。注销 XFCE 后自动重新启动 Win95 桌面。
- **首次安装**：若缺少 Xorg/XFCE/libinput/udev，切换脚本会在交互式终端里说明需要安装的 Alpine 包，并询问是否执行 `apk add`；首次约需数百 MB。
- **鼠标键盘**：XFCE 使用 `xf86-input-libinput`。启动前会从 `/sbin/udevd`、`/lib/udev/udevd` 等路径启动 udev（eudev 的守护进程不在 PATH 中），再执行 `udevadm trigger` 和 `udevadm settle`，确保输入设备带上 `ID_INPUT` 标记。没有这些标记时 libinput 不会接管设备，表现正是“XFCE 画面出来了但鼠标键盘全死”。
- **诊断日志**：`/var/log/xfce.log` 记录 `/dev/input`、每个事件设备的 udev 标记、`startxfce4` 输出、`xinput list` 与 Xorg 的 libinput/输入相关行。切换后点不动时，先看这个文件里 `xinput list` 和设备 `ID_INPUT_*` 是否为空。

### X11 应用与统一通知

- **XFCE 终端 / Firefox**：从桌面图标、开始菜单或运行对话框启动后，程序画面出现在代理窗口中；窗口工具栏提供“终端”“浏览器”“重连”快捷按钮。
- **性能提示**：UTM Apple 虚拟化没有 GPU，Firefox 使用 Mesa llvmpipe 软件渲染，首次冷启动和复杂页面会较慢；代理固定 24 位色、1024×768，适合轻量网页和 mcjs.link 的 WebGL2 演示。
- 统一通知系统把后台事件送到 Win95 托盘：
  - 各脚本通过 `/usr/bin/win95-notify "标题" "内容"` 追加到 `/run/win95-notify`；Win95 桌面每秒读取该队列，在任务栏右下角显示黄色气泡，约 8 秒后自动消失。
  - `win95-download-watch` 会监视 `/root/下载`，新文件出现后发送“下载完成”通知；X11 程序退出时也会发送通知。
  - 通知队列不会因为代理窗口打开而丢失。

> Office 组件按需安装，其余新功能随系统镜像安装。26.3 首次引入 WIN95SUPER 超级分区与 A/B 双插槽；已安装 26.4 的系统可直接通过 OTA 升级到 26.5，不必重装。更早的单根分区版本仍需用安装镜像重装一次，之后即可持续 OTA 更新。

### 系统更新（OTA）

- **版本关系**：本次发布的系统版本是 **26.5**，已内置更新客户端，26.5 的安装镜像、更新包、SHA-256 与 `latest.json` 随本次 Release 全部上传。26.4 系统打开“系统更新”即可下载并升级到 26.5。版本号按“26.x → 27.0”递增：26.5 之后的下一个大版本是 **27.0**，不会出现 26.6。
- **入口**：开始 → 设置 → **Windows 更新**，或 开始 → 程序 → **系统更新**。窗口显示当前版本、运行插槽、状态、进度、更新说明与日志。
- **检查更新**：客户端读取
  `https://github.com/xiaobai-win95/win95-linux/releases/latest/download/latest.json`，
  比较版本号并显示更新说明。也可以按 F5 或菜单“更新 → 检查更新”。
- **国内网络与镜像**：默认**并行探测** GitHub 直连与公共加速镜像（`gh-proxy.com`、`ghfast.top`、`ghproxy.net`、`ghproxy.cn`），清单以最先返回的有效源为准；下载前先用 1 字节请求测速，按快慢依次尝试，失败自动换源并沿用 `.part` 续传。单个源超时限制：清单 12 秒、连接 8 秒、下载连续 45 秒无数据即切换。
  可在 `/etc/win95-ota.conf` 自定义：
  - `MANIFEST_URL=<完整地址>`：改用自建/Gitee 等清单；
  - `MIRROR=<前缀或含 {url} 的模板>`：可写多行，写了就替换内置镜像列表；
  - `DIRECT=0`：跳过 GitHub 直连，减少等待。
  修改后无需重启，下次检查/下载即生效。
- **下载并安装**：
  1. 更新包保存到超级分区的 `cache/`，支持断点续传（`.part`）；
  2. 按清单中的 SHA-256 与大小校验更新包；
  3. 在当前未运行的插槽中建立当前系统的**硬链接快照**，再把新系统的 `rootfs.tar.gz` 解压覆盖进去。未修改的文件继续共享磁盘块，用户文档、下载、自己安装的软件和 Arch 子系统都会保留；
  4. 写入 `/etc/fstab`、`/etc/win95-version` 与 `ota/state.json`，把新插槽标记为“试用”。当前运行的插槽全程不被修改。
- **重启生效**：安装完成后点“立即重启”。引导程序（bootinit）读取 `/mnt/win95-super/ota/state.json`，优先启动试用插槽；桌面成功运行约 8 秒后自动确认该插槽。连续两次启动都未能确认时，下一次启动自动回退到旧插槽，并把失败插槽标记为 `failed`。
- **磁盘布局**：
  ```
  WIN95SUPER (ext4)
  ├── slots/a       正在运行或旧的根文件系统
  ├── slots/b       备用根文件系统
  ├── ota/state.json  active / trial / attempts / booted / 各插槽版本
  └── cache/        OTA 更新包与解包临时目录
  ```
  两个插槽共享整个超级分区的剩余空间，不需要预先划分固定大小的 B 分区。安装器与更新客户端占用的是“当前系统 + 用户数据 + 一个更新包”的空间。
- **版本文件**：`/etc/win95-version` 包含 `VERSION=`、`BUILD=`、`SLOT_LAYOUT=super-v1`；终端可用 `cat /etc/win95-version` 查看。
- **GitHub 发布约定（26.4 起）**：每个 Release（标签如 `v26.5`）上传完整交付物：
  - `win95-installer-aarch64.img`、`win95-installer-aarch64.img.sha256`；
  - `win95-ota-26.5-aarch64.tar.gz`、`win95-ota-26.5-aarch64.tar.gz.sha256`；
  - `latest.json`（内容由构建脚本生成，`package.url` 指向该 Release 的更新包）。
  **26.5 安装镜像、更新包、SHA256 与 latest.json 全部上传**；已安装 26.4 的虚拟机可直接 OTA 升级，新虚拟机用 26.5 镜像全新安装。
  只有挂在 **latest release** 上的 `latest.json` 会被 `releases/latest/download/` 解析；客户端不读取 Release 正文。
  **本机构建也会在 `out/` 留下旧版本的更新包与清单，不要上传**；26.5 的 Release 发布后，其 `latest.json` 才会被客户端读取。
  构建 26.5 时可用 `VERSION=26.5 OTA_NOTES="更新说明…" ./build-installer.sh` 生成对应版本的镜像、更新包与清单。下一大版本号为 27.0。
- **与 Android 虚拟 A/B 的差别**：本系统没有 Android 的 liblp 超级分区元数据、dm-linear / dm-snapshot、update_engine、boot_control HAL 与 AVB/dm-verity 验证链。这里用 ext4 目录 + 文件级硬链接快照实现同样的用户体验（共享超级分区、后台写备用槽、试用与自动回退），不是 Android 的块级快照。OTA 只替换根文件系统；内核、initrd 与 `BOOTAA64.EFI` 仍在 EFI 分区上，不随 OTA 更新。如果未来版本需要换内核，需发布新的安装镜像。
- **离线或 GitHub 不可达**：检查更新会显示失败原因，不影响当前系统；可以照常使用。更新包 SHA-256 校验失败会拒绝安装。

---

## 七、常见问题

**启动后黑屏，或没有图形界面**

- 分辨率不是限制条件，任意分辨率都可以使用；若黑屏，先确认显示设置里的 **OpenGL** 已开启（如果有该选项）。
- 高分辨率下界面元素会显得更小，可调低虚拟机窗口缩放或分辨率，不影响功能。
- 安装盘启动时若长时间黑屏，等待 15 秒；安装器会等待显示设备出现。
- 已安装系统启动时，引导程序会在屏幕上显示 `win95-boot:` 开头的进度或错误信息；若启动失败，原因会停留在屏幕上，而不是单纯黑屏。
- 已安装系统启动日志写入 `/var/log/win95gui.log`，可在终端中查看；若图形界面反复重启，先检查显示设置。
- 图形界面启动失败时，屏幕会直接显示退出码、日志尾部、`/dev/fb*` 与输入设备列表，并进入一个 shell；把这段内容发我即可定位。输入 `exit` 会重新尝试启动图形界面。

**按 Ctrl+Alt+F2 或执行 `chvt 2` 后画面不动、终端卡死**

- 这是早期镜像的缺陷，已在本镜像修复：GUI 现在通过 `VT_PROCESS` 响应内核的 VT 切换信号，切走时关闭 `/dev/fb0`，切回时重新打开并恢复桌面。
- 已安装的旧系统只替换 `/initrd` **不够**，因为 `win95gui` 在系统本体里，需要重新安装或至少把新版 `win95gui` 复制进系统盘。
- 切换后 tty2/tty3 显示的是 BusyBox `askfirst` 提示而不是 `login:`，按一次回车即可得到 root shell，这是预期行为。

**鼠标或键盘无反应**

- 确认输入设备为 **USB**，不要添加 PS/2。
- 安装器启动时会等待键盘和鼠标设备最多 6 秒。
- 在 UTM 设置中把指针设备设为 **USB Tablet** 或默认 USB 鼠标。
- 扫雷插旗使用鼠标右键：Mac 触控板请用**双指点按**；若只识别到左键，可改用外接鼠标测试。

**切换到 XFCE 后画面能出来，但鼠标键盘点不动**

- 这是 libinput 没有拿到带 udev 标记的输入设备。26.3 已修复：`startgui` 会从 `/sbin/udevd`、`/lib/udev/udevd`、`/usr/lib/udev/udevd` 等实际路径启动 eudev（不再只查 PATH），等待 `/run/udev/control` 就绪后执行 `udevadm trigger --action=add` 与 `udevadm settle`。
- 如果仍然无输入，切到 tty2（Ctrl+Alt+F2，按回车）查看日志：
  ```sh
  grep -E 'udev|ID_INPUT|libinput|xinput' /var/log/xfce.log
  grep -iE 'libinput|input|\(EE\)' /var/log/Xorg.0.log | tail -n 40
  ```
  `xinput list` 为空说明 X 没有输入设备；`ID_INPUT_*` 为空说明 udev 没打标签。确认已安装 `eudev`、`xf86-input-libinput` 和 `xinput`：
  ```sh
  apk add eudev xf86-input-libinput xinput
  /usr/bin/switch-to-xfce
  ```
- 确认 UTM 的指针设备是 **USB Tablet**（或默认 USB 鼠标）；PS/2 设备在部分 Apple 虚拟化配置下不会被自动识别。
- 桌面上找不到“切换到 XFCE 桌面”时，确认镜像为 26.5 且没有被旧版快捷方式缓存覆盖；开始 → 程序 → “切换到 XFCE 桌面”与桌面图标使用同一个入口，也可以直接执行 `/usr/bin/switch-to-xfce`。

**微信报 libandromeda.so 或其它共享库找不到**

- 先按第六节“微信 WeChat”的“libandromeda.so 报错的手动修复”走一遍；自动安装失败时重新点“微信 WeChat”也会触发修复。
- 关键点：deb 必须复制进 `/opt/archlinux` 再解压；解压后要对主程序和所有 `.so*` 执行 `patchelf --clear-execstack` 和 `patchelf --set-rpath`，再运行 `ldconfig`；`/tmp/.X11-unix`、`/run` 需要通过 `win95-arch-mounts` 绑定进 chroot，否则 X11 和 DBus 连接也会失败。
- 用 `ldd /opt/wechat/wechat | grep 'not found'` 检查剩余依赖，并按报错用 pacman 补装。

**屏保不出现，或出现后不能退出**

- 默认在连续 **3 分钟**无输入后触发；可在“显示属性 → 屏幕保护程序”中改为 1–120 分钟，或选择“无”关闭屏保。
- 如果刚改过等待时间，需要重新计时：操作一次鼠标或键盘后再等待。
- 任意鼠标移动或按键即可退出屏保。UTM 窗口必须先获得焦点，否则输入事件不会送达虚拟机。
- 屏保只覆盖画面，不锁屏、不中断终端里正在运行的程序。

**Win95 与 XFCE 之间剪贴板不同步**

- 先确认 XFCE 会话里 `win95-clipd` 正在运行：`pgrep -a win95-clipd`；没有则执行 `win95-clipd &`，或检查 `/root/.config/autostart/win95-clipd.desktop`。
- 需要 `xclip`：`apk add xclip`。`startgui` 在启动 XFCE 前会自动检查并补装。
- 剪贴板文件是 `/run/win95-clipboard`，权限 0644；可在终端 `cat` 查看当前内容。
- 空内容不会被同步，这是为了避免清空另一侧的已有内容。

**应用商店装完软件，桌面没有出现快捷方式**

- 包内带 `/usr/share/applications/*.desktop` 的图形软件会按桌面入口生成快捷方式；没有 `.desktop`、但包名对应可执行文件的命令行程序，会生成一个 `Terminal=true` 的终端快捷方式。
- 确认安装成功（包管理器日志里出现 `OK:`），并确认 `/root/桌面` 存在。
- 桌面每 3 秒检查一次 `/root/桌面` 的目录变化并自动刷新图标；也可以在终端执行 `ls /root/桌面` 检查。
- 标记为 `NoDisplay=true` 或 `Hidden=true` 的入口不会生成图标。
- 双击图形程序快捷方式会在 X11 嵌套代理窗口中启动；缺少组件时会先弹出安装确认，这是预期行为。

**双击“XFCE 终端”或“Firefox”没有反应，或提示未安装**

- 首次使用会弹出组件安装确认，选择“确定”后由 `win95-x11-setup` 自动执行 `apk add`（`xvfb`、`dbus`、`xfce4-terminal`、`firefox`、`font-wqy-zenhei`、Mesa 等），进度显示在“安装进度”窗口；完成后会自动启动刚才请求的程序。
- 若安装失败，查看 `/var/log/win95-x11-setup.log`；也可在 Win95 终端手动执行：
  ```sh
  apk update && apk add xvfb dbus dbus-x11 xfce4-terminal firefox font-wqy-zenhei mesa mesa-egl mesa-gles mesa-gl mesa-dri-gallium
  ```
  然后重新双击图标。
- 代理窗口空白或一直停在连接状态：查看 `/tmp/win95-xvfb.log`，确认 `pgrep -af Xvfb` 有进程；点工具栏的“重连”会清理失效的 `/tmp/.X1-lock` 并重新启动虚拟显示。
- 程序启动失败时查看 `/tmp/win95-x11-app.log`。
- 同一时间只使用一个代理窗口；重复打开会复用已有窗口，不会切换控制台。

**微信第一次启动很慢、失败，或提示空间不足**

- 微信首次运行需要依次下载：Arch Linux ARM 根文件系统（约 800 MB）、Xvfb/Mesa（约 200 MB）、微信官方 ARM64 安装包（约 200 MB），以及 pacman 运行库（约几百 MB）。总下载量约 1.5 GB，视网络情况可能需要 10–40 分钟。
- 所需空闲磁盘约 5 GB；建议目标磁盘 16 GiB。可用 `df -h /opt` 检查。
- 如果 `pacman` 或安装脚本提示空间不足，日志现在会记录操作前的实际可用空间。Arch 解压后约 2 GB，微信运行库与缓存另需约 1.5–2 GB。安装器会按**安装时的磁盘大小**把根分区一直做到磁盘末尾；如果安装完成后才扩大虚拟磁盘，旧分区不会自动变大，需要重新安装，或先用分区工具扩展根分区。
- 下载支持断点续传；失败后再次点击“微信 WeChat”或“Arch Linux”会继续。Arch 基础包会按 TUNA → 中科大 → 北外 → 美国 FL → 官方自动换源，HTTP 状态码与错误会写入日志。如果所有镜像都返回 403（常见于镜像站拦截该出口 IP 或拦截 BusyBox 的请求），可手动导入：
  1. 在宿主机下载 `ArchLinuxARM-aarch64-latest.tar.gz`（约 800 MB）；
  2. 把文件放进虚拟机的 `/root/下载/`；
  3. 重新点击“Arch Linux”，脚本会校验 gzip 后自动导入缓存，不再联网下载。
- 如果 `pacman` 报 `error adding trust anchors from file: /etc/ssl/certs/ca-certificates.crt`、`failed to initialize alpm library ... /var/lib/pacman/`，或提交事务时报 `could not open file: /etc/mtab`、`could not determine filesystem mount points`、`could not determine cachedir mount point`，说明 chroot 处于半初始化状态。运行 `win95-arch-repair`（或开始菜单 Arch → **检查并修复**）即可：它会补齐 `local`/`sync` 目录、写入带 `/` 根挂载项的 `/etc/mtab`、从缓存的压缩包恢复被删掉的包数据库、用 Alpine 的 Mozilla 根证书包修复 SSL，并在 HTTPS 失败时自动把 TUNA 源切换为 HTTP。紧急情况下也可在 Alpine 终端（退出 chroot 后）手动执行：
  ```sh
  mkdir -p /opt/archlinux/var/lib/pacman/local /opt/archlinux/var/lib/pacman/sync
  # pacman 提交事务前要读 /etc/mtab，里面必须有一行挂载点是 "/"
  printf 'rootfs / rootfs rw,relatime 0 0\n' > /opt/archlinux/etc/mtab
  cp -Lf /etc/ssl/certs/ca-certificates.crt /opt/archlinux/etc/ssl/certs/ca-certificates.crt
  cp -Lf /etc/ssl/certs/ca-certificates.crt /opt/archlinux/etc/ssl/cert.pem
  printf 'Server = https://mirrors.tuna.tsinghua.edu.cn/archlinuxarm/$arch/$repo\n' \
    > /opt/archlinux/etc/pacman.d/mirrorlist
  # 本地包数据库丢失时，从缓存的官方压缩包里挑出 /var/lib/pacman/local 还原：
  TAR=/var/cache/win95/ArchLinuxARM-aarch64-latest.tar.gz
  entry=$(tar -tzf "$TAR" | grep -m1 'var/lib/pacman/local') || true
  if [ -n "$entry" ]; then
    prefix=${entry%%var/lib/pacman/local*}var/lib/pacman/local
    tar -xzf "$TAR" -C /opt/archlinux "$prefix"
  fi
  ```
  然后重新进入 chroot 执行 `pacman -Syy`；HTTPS 仍报证书错误时把 mirrorlist 的 `https://` 改成 `http://`。缓存压缩包也没有时，重新点“Arch Linux”由脚本下载并解压基础系统。
  - `/var/log/win95-arch-bootstrap.log`（Arch 子系统）
  - `/var/log/win95-arch-repair.log`（证书、pacman 数据库修复）
  - `/var/log/win95-wechat-setup.log`（微信安装）
- Arch 子系统装在 `/opt/archlinux`，官方微信装在 `/opt/archlinux/opt/wechat`。安装完成后 `pacman -Q` 可查看已安装的运行库。
- 微信在 Xvfb `:1` 的 X11 嵌套代理窗口中运行；首次登录请用手机扫码。若窗口没有出现，查看 `/tmp/win95-xvfb.log` 与 `/tmp/win95-x11-app.log`。

**方块世界 3D 画面卡顿，或想用 GPU**

- 3D 渲染器为自研体素光线步进，完全由 CPU 绘制；在 UTM Apple 虚拟化下没有 GPU 可直通，这是预期行为。窗口越小、像素越少，帧率越高。
- Mesa/llvmpipe 的软件 OpenGL 只提供给 X11 代理中的程序（Firefox 等），Win95 桌面的方块世界不使用 GL。
- 如需更高帧率，可缩小方块世界窗口，或在高性能 Apple Silicon 上运行；Win95 桌面本身不使用 GL，X11 代理中的 Firefox 才使用 Mesa/llvmpipe。

**mcjs.link 的 Minecraft 打不开**

- 浏览器内核只支持 ECMAScript 5.1，无法运行 WebGL2/WASM；打开 mcjs.link 时会显示说明页，并给出“在 Firefox 中打开”按钮。
- Firefox 由 X11 嵌套代理按需安装（`win95-x11-setup` 会一并安装 Xvfb、Firefox 与 Mesa）；也可手动执行 `apk add xvfb firefox mesa mesa-egl mesa-gles mesa-gl mesa-dri-gallium`。
- Firefox 使用 `LIBGL_ALWAYS_SOFTWARE=1` 的 llvmpipe 软件 WebGL2，加载地图可能较慢，请耐心等待；性能取决于 CPU。

**通知气泡不出现**

- 气泡只在 Win95 桌面可见时显示；在代理窗口运行 X11 程序或切换控制台期间，通知会留在 `/run/win95-notify` 队列中，回到桌面后补弹。
- 检查队列文件是否存在：`ls -l /run/win95-notify`；写入测试通知：`win95-notify 测试 这是一条通知`。
- XFCE 下载监视由 `win95-download-watch` 负责，确认进程在运行：`pgrep -af win95-download-watch`；没有则手动执行 `/usr/bin/win95-download-watch &`。
- 一条通知约 8 秒后自动消失，连续事件会依次显示。

**Print Screen 没有反应，或找不到截图**

- 先点一下 UTM 窗口，确保键盘事件送进虚拟机；部分 Mac 键盘需要按 `Fn+Print` 或外接键盘。
- 截图保存在 `/root/桌面`，每次同时生成 `截图-20261031-134500.png` 和同名 `.bmp`，并自动在画图中打开 PNG。也可以在“开始 → 设置 → 截屏”手动触发。
- 保存成功会弹出“截图已保存（PNG + BMP）”气泡；失败时会弹出错误框。

**没有开机“滴”声**

- 系统会尝试向 `/dev/tty0` 或 `/dev/console` 写入响铃字符。UTM / Apple 虚拟化通常不暴露 PC 喇叭，因此没有声音是正常现象，不影响其他功能。

**开始菜单里的关机/重启没有反应**

- 确认使用的是最新镜像；GUI 以非 PID 1 身份运行，必须通过 `/sbin/poweroff`、`/sbin/reboot` 执行关机。
- 相关日志在 `/var/log/win95gui.log`；安装器本身是 PID 1，关机走另一条路径。

**安装程序里看不到目标磁盘**

- 确认已添加第二块 VirtIO 磁盘，且大小 ≥ 1 GiB。
- 检查新建磁盘时 **“使用 NVMe 磁盘接口” 没有勾选**；系统启动镜像不含 NVMe 驱动。
- 检查目标盘的 **“只读?” 没有勾选**。
- 点击安装程序的 **刷新**。
- 如果目标盘仍显示为安装盘，说明两块盘选反了；安装盘带有 `WIN95INST` 分区标签。

**安装中途报错（等待分区设备超时、mke2fs 失败 exit status 127 等）**

- 先核对镜像 SHA-256 是否与本文一致；不一致说明 UTM 仍在用旧镜像，请完全关机后用新文件覆盖原路径，或重新导入。
- 本镜像已包含修复：写分区表后通知内核重新扫描、安装环境补齐 `libeconf` 等依赖库。
- 失败的目标盘上会残留半截分区表，无需手工清理，重装时安装程序会重新清空并重写。

**已安装系统停在 `win95-boot: FATAL: ... /sbin/init` 或屏幕上出现一串方块字**

- 这是 `5b88d3aa…` 及更早镜像的引导缺陷：bootinit 在切换根目录之前检查 `/sbin/init`，而该路径是指向绝对路径 `/bin/busybox` 的符号链接，在 initramfs 里解析不到，于是判定根文件系统缺少 init 并停机。
- 屏幕上的方块字是中文提示：Linux 文本控制台没有 CJK 字体，因此新版引导信息全部改用英文显示。
- 换用 `2d79d0d7…` 或更新的镜像即可修复。有两条路：

  **A. 不重装，只替换启动镜像**（已经装好系统时用这个，最快）

  出问题的只是目标盘 EFI 分区里的 `/initrd`，系统本体完好。完全关机 → 用新镜像覆盖 UTM 里的安装盘文件 → 启动到安装器 → 点 **终端**，执行：

  ```sh
  mkdir -p /mnt/esp
  mount /dev/vda1 /mnt/esp          # 目标盘的 EFI 分区；若目标盘是 vdb 则写 /dev/vdb1
  cp /payload/initrd-installed.img /mnt/esp/initrd
  sync
  umount /mnt/esp
  ```

  然后关机、按本文第五节移除安装盘，正常启动。

  **B. 重新安装一次**：重装会连同新的引导镜像一起重写，选择 10 GiB 的那块盘即可。

**终端里提示“当前运行的内核在编译时没有启用 CONFIG_PACKET 选项”，或一直没有 IP 地址**

- 这句话有误导性：内核其实支持，只是 Alpine 的 lts 内核把 `CONFIG_PACKET` 编译成了模块 `af_packet.ko`。已安装系统的模块全部来自 EFI 分区里的 `/initrd`，旧镜像没有打包这个模块，于是 BusyBox 的 `udhcpc` 建不出 PF_PACKET 套接字，DHCP 拿不到地址，浏览器也就打不开网页。
- `2d79d0d7…` 起已把 `af_packet.ko` 放进启动镜像。修复方式与上面相同：按 **A** 用新 `/initrd` 覆盖 EFI 分区里那一份并重启，不需要重装。
- 修复后在终端确认：`ifconfig` 里 eth0 有地址（通常是 `192.168.x.x` 或 `10.x.x.x` 的局域网地址），`cat /etc/resolv.conf` 非空。

**启动菜单超时直接进入系统**

- 正常行为：`loader.conf` 设置为 3 秒超时并默认启动第一项。

**浏览器打不开网页**

- 检查 UTM 网络是否为 **桥接 / Bridged**，桥接接口是否为 **Wi-Fi**；不要选 Ethernet/以太网。
- 在终端执行 `ifconfig` 确认已获取与 Mac 同网段的地址，`cat /etc/resolv.conf` 确认 DNS。
- 如果 eth0 根本没有地址，且终端出现过“内核没有启用 CONFIG_PACKET 选项”的提示，是 `/initrd` 里缺 `af_packet.ko`，按前面那条处理。
- HTTPS 站点使用系统内置 CA 证书；若站点证书异常，浏览器会显示错误信息。

**软件包管理器搜索不到软件，或安装失败**

- 软件包管理器需要联网。首次打开会先执行 `apk update`，日志里出现 `fetch https://mirrors.tuna.tsinghua.edu.cn/alpine/v3.24/...` 和 `OK:` 才表示索引下载成功。
- 若日志提示网络错误，先在终端确认地址：`ifconfig` 应有与 Mac 同网段的局域网地址（如 `192.168.x.x`），`cat /etc/resolv.conf` 应有 `nameserver`。没有地址时见上一条 `CONFIG_PACKET` 的说明。
- 清华镜像不可用或同步滞后时，可切回官方源，在终端执行：

  ```sh
  sed -i 's|mirrors.tuna.tsinghua.edu.cn|dl-cdn.alpinelinux.org|' /etc/apk/repositories
  apk update
  ```

  改回清华源只需把 `sed` 里的两个域名对调。
- 软件包管理器（apk）只管理 Alpine（musl）v3.24 仓库中的包。需要 Arch/glibc 软件时，请在开始菜单安装 **Arch Linux (glibc / pacman)**，然后用 `pacman -S 包名`（清华 TUNA 源）；两个包管理器互不影响。
- X11 图形程序由“X11 嵌套代理”在 Win95 窗口中运行（Xvfb `:1`，不需要切换控制台）；包管理器生成图形快捷方式后直接双击即可，开始 → 运行里也能输入程序名。仅支持 Wayland 的程序无法在代理中运行；命令行程序仍可在 Win95 终端正常使用。
- 安装内容写入系统盘，重启后保留；目标盘只有 8–10 GiB 时，避免安装微信/Arch 这类体积较大的软件。

**想恢复干净状态**

- 删除该虚拟机的目标磁盘，重新创建一块 16 GiB raw 磁盘（只装桌面可用 8 GiB），再按本说明安装。

---

## 八、说明

- 安装镜像与已安装系统均为 ARM64 UEFI，面向 Apple Virtualization / UTM。
- 本说明中的镜像由离线素材构建；构建者未在 UTM 中实际启动过虚拟机，运行时行为需要按上述步骤自行验证。
- UTM 的 Apple Virtualization 后端只支持 raw 磁盘；qcow2 仅适用于 QEMU 后端。

参考：UTM 官方文档《Drive》（Apple Virtualization）  
https://docs.getutm.app/settings-apple/drive/
