package desktop

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func savePreview(t *testing.T, c *ui.Canvas, name string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			v := c.Pix[y*c.Stride+x]
			img.SetRGBA(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
		}
	}
	f, err := os.Create("/Users/madic/aiwork/win95linux/out/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewFrames(t *testing.T) {
	_ = os.MkdirAll("/Users/madic/aiwork/win95linux/out", 0o755)
	info := "预览环境 · 1024×768"

	d := New(1024, 768, ModeDesktop, Host{Info: info})
	canvas := ui.NewCanvas(1024, 768)
	d.clock = "09:41"
	d.cpuPct, d.memPct = 37, 64
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-desktop.png")

	d.startOpen = true
	d.startSub = "prog"
	d.startSel = "prog"
	d.startHover = "notepad"
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-start.png")
	d.startOpen = false

	d.openContext(d.desktopMenuItems(), 300, 300)
	d.ctx.hover = 3
	d.ctx.sub = 3
	d.ctx.layoutSub(d)
	d.ctx.subHover = 0
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-context-desktop.png")
	d.ctx = nil

	d.openContext(d.iconMenuItems(0), 24, 20)
	d.ctx.hover = 0
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-context-icon.png")
	d.ctx = nil

	for i, ic := range d.icons {
		if ic.key != "sys:recycle" {
			continue
		}
		d.openContext(d.iconMenuItems(i), 300, 300)
		d.ctx.hover = 6
		d.Draw(canvas)
		savePreview(t, canvas, "desk-preview-context-recycle.png")
		d.ctx = nil
		break
	}

	np := NewNotepadApp(d, "")
	np.SetContent("/root/文档/工作计划.txt", "本周工作计划\n\n1. 完成桌面环境的中文化与图标绘制。\n2. 验证终端、计算器和日历程序。\n3. 检查浏览器对本地页面和下载链接的支持。\n4. 在 UTM 中安装到虚拟硬盘并测试启动。\n\n备注：所有界面使用 Win95 经典配色。\n")
	nw := d.Open(np)
	np.win = nw
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-notepad.png")

	np.selAnchorLn, np.selAnchorCol = 2, 0
	np.caretLn, np.caretCol = 2, 8
	np.selActive = true
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-notepad-selection.png")
	np.selActive = false

	d.openContext(d.windowMenuItems(nw), nw.X+20, nw.Y+40)
	d.ctx.hover = 7
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-window-menu.png")
	d.ctx = nil

	term := &TerminalApp{d: d}
	term.t = &termScreen{
		cols: 80, rows: 24, fg: 0xC0C0C0, bg: 0x000000, cursor: true,
		inputCh: make(chan []byte, 8), done: make(chan struct{}),
	}
	term.t.resetGrid()
	term.t.writeText("Welcome to Windows 95 Linux (aarch64)\r\n" +
		"win95 login: root\r\n" +
		"\x1b[1;32mroot@win95\x1b[0m:\x1b[1;34m/root\x1b[0m$ uname -a\r\n" +
		"Linux win95 6.18.58-0-lts #1 SMP aarch64 Linux\r\n" +
		"\x1b[1;32mroot@win95\x1b[0m:\x1b[1;34m/root\x1b[0m$ ls\r\n" +
		"\x1b[1;34m文档\x1b[0m  \x1b[1;34m下载\x1b[0m  \x1b[1;34m桌面\x1b[0m\r\n" +
		"\x1b[1;32mroot@win95\x1b[0m:\x1b[1;34m/root\x1b[0m$ _\r\n")
	tw := d.Open(term)
	_ = tw
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-terminal.png")

	calc := NewCalcApp()
	d.Open(calc)
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-calc.png")

	br := NewBrowserApp(d, "about:home")
	bw := d.Open(br)
	br.win = bw
	time.Sleep(150 * time.Millisecond)
	d.RunPosted()
	br.Navigate("about:home")
	time.Sleep(100 * time.Millisecond)
	d.RunPosted()
	d.Draw(canvas)
	savePreview(t, canvas, "desk-preview-browser.png")

	id := New(1024, 768, ModeInstaller, Host{Info: info})
	id.installer.disks = []diskInfo{
		{path: "/dev/vda", model: "QEMU HARDDISK", sizeBytes: 512 << 20, installer: true, hasTable: true, parts: 1},
		{path: "/dev/vdb", model: "VirtIO Block Device", sizeBytes: 8 << 30, hasTable: false},
	}
	id.installer.sel = 1
	id.installer.err = ""
	id.installer.scroll.Total = 2
	ic := ui.NewCanvas(1024, 768)
	id.Draw(ic)
	savePreview(t, ic, "desk-preview-installer.png")

	id.installer.stage = instRunning
	id.installer.step = 4
	id.installer.pct = 43
	id.installer.appendLog("目标磁盘: /dev/vdb (8.0 GB)")
	id.installer.appendLog("正在写入 GPT 分区表...")
	id.installer.appendLog("正在格式化 EFI 系统分区（FAT32）...")
	id.installer.appendLog("正在格式化 Linux 系统分区（ext4）...")
	id.installer.appendLog("已解压 132 个文件")
	id.Draw(ic)
	savePreview(t, ic, "desk-preview-install-progress.png")

	// New applications, each on a clean desktop so the layout is easy to check.
	renderApp(t, "desk-preview-pkgman.png", func(d *Desktop) App {
		installed := map[string]pkgEntry{
			"musl":      {Name: "musl", Version: "1.2.5-r0", Desc: "the musl c library (libc) implementation", Installed: true},
			"busybox":   {Name: "busybox", Version: "1.37.0-r12", Desc: "Size optimized toolbox of many common UNIX utilities", Installed: true},
			"apk-tools": {Name: "apk-tools", Version: "3.0.8-r0", Desc: "Alpine Package Keeper", Installed: true},
			"nano":      {Name: "nano", Version: "8.2-r0", Desc: "Advanced text editor", Installed: true},
		}
		return &PkgManApp{
			d: d, sel: 10, installed: installed, lastIndex: -1, focus: 1,
			query: []rune("editor"), caret: 6,
			items: []pkgEntry{
				{Name: "ed", Version: "1.21-r0", Desc: "Line-oriented text editor"},
				{Name: "emacs", Version: "29.4-r3", Desc: "Extensible, customizable text editor"},
				{Name: "helix", Version: "24.07-r2", Desc: "Post-modern modal text editor"},
				{Name: "joe", Version: "4.6-r0", Desc: "Full featured terminal-based text editor"},
				{Name: "kakoune", Version: "2024.05.18-r0", Desc: "Modal text editor"},
				{Name: "mg", Version: "20240709-r0", Desc: "Micro GNU/emacs"},
				{Name: "micro", Version: "2.0.14-r2", Desc: "Modern and intuitive terminal-based text editor"},
				{Name: "nano", Version: "8.2-r0", Desc: "Advanced text editor", Installed: true},
				{Name: "ne", Version: "3.3.3-r0", Desc: "The Nice Editor"},
				{Name: "neovim", Version: "0.10.2-r0", Desc: "Vim-fork focused on extensibility and usability"},
				{Name: "vim", Version: "9.1.0900-r0", Desc: "Vi IMproved: a highly configurable text editor"},
				{Name: "vis", Version: "0.9-r1", Desc: "Modern, legacy-free vi-like editor"},
			},
			status: "找到 12 个与“editor”相关的软件包",
			log: []string{
				"$ apk update",
				"fetch https://mirrors.tuna.tsinghua.edu.cn/alpine/v3.24/main/aarch64/APKINDEX.tar.gz",
				"OK: 12458 distinct packages available",
			},
		}
	}, nil)
	renderApp(t, "desk-preview-mines.png", func(d *Desktop) App {
		m := newMineApp(0)
		m.d = d
		m.state = minePlaying
		m.start = time.Now().Add(-42 * time.Second)
		m.elapsed = 42
		for _, i := range []int{48, 51, 56, 59, 64, 67, 70, 72, 75, 78} {
			m.mine[i] = true
		}
		m.recount()
		m.reveal(0)
		for _, i := range []int{56, 64, 78} {
			m.toggleFlag(i)
		}
		return m
	}, nil)
	renderApp(t, "desk-preview-paint.png",
		func(d *Desktop) App { return NewPaintApp(d) },
		func(d *Desktop, app App) {
			p := app.(*PaintApp)
			p.brush = 3
			p.color = 0x000080
			p.rect(30, 90, 260, 230, 2, 0x000080)
			p.line(20, 92, 145, 30, 3, 0x800000)
			p.line(145, 30, 270, 92, 3, 0x800000)
			p.rect(70, 130, 120, 180, 2, 0x008080)
			p.rect(170, 130, 220, 180, 2, 0x008080)
			p.rect(125, 170, 165, 230, 2, 0x000000)
			p.ellipse(330, 55, 390, 115, 3, 0xFF8000)
			p.ellipse(420, 130, 560, 220, 3, 0x00A000)
			p.line(30, 250, 600, 250, 3, 0x008000)
			for x := 30; x < 600; x += 9 {
				p.line(x, 250, x-6, 240, 1, 0x00A000)
			}
			p.color = 0xC00000
			p.status = "坐标 312, 188"
		})

	menuD := New(1024, 768, ModeDesktop, Host{Info: info})
	menuD.clock = "09:41"
	menuD.cpuPct, menuD.memPct = 37, 64
	menuD.startOpen = true
	menuD.startSel = "shutdown"
	mc := ui.NewCanvas(1024, 768)
	menuD.Draw(mc)
	savePreview(t, mc, "desk-preview-menu-power.png")

	taskD := New(1024, 768, ModeDesktop, Host{Info: info})
	taskD.Open(NewCalcApp())
	taskD.Open(NewCalendarApp())
	taskD.clock = "09:41"
	taskD.openContext(taskD.taskbarMenuItems(), 420, 740)
	taskD.ctx.hover = 0
	tc := ui.NewCanvas(1024, 768)
	taskD.Draw(tc)
	savePreview(t, tc, "desk-preview-taskbar-menu.png")

	saverD := New(1024, 768, ModeDesktop, Host{Info: info})
	now := time.Now()
	saverD.startSaver(now)
	for i := 1; i <= 240; i++ {
		saverD.stepSaver(now.Add(time.Duration(i) * 33 * time.Millisecond))
	}
	sc := ui.NewCanvas(1024, 768)
	saverD.Draw(sc)
	savePreview(t, sc, "desk-preview-saver.png")

	starD := New(1024, 768, ModeDesktop, Host{Info: info})
	starD.saverStyle = saverStarfield
	starD.startSaver(now)
	for i := 1; i <= 300; i++ {
		starD.stepSaver(now.Add(time.Duration(i) * 33 * time.Millisecond))
	}
	stc := ui.NewCanvas(1024, 768)
	starD.Draw(stc)
	savePreview(t, stc, "desk-preview-saver-starfield.png")

	for _, bg := range []struct {
		style int
		name  string
	}{{3, "cloud"}, {4, "night"}, {5, "slate"}} {
		wallD := New(1024, 768, ModeDesktop, Host{Info: info})
		wallD.clock = "09:41"
		wallD.cpuPct, wallD.memPct = 37, 64
		wallD.background = bg.style
		wc := ui.NewCanvas(1024, 768)
		wallD.Draw(wc)
		savePreview(t, wc, "desk-preview-wallpaper-"+bg.name+".png")
	}

	dispD := New(1024, 768, ModeDesktop, Host{Info: info})
	dispD.background = 0
	dispD.openDisplayProperties()
	dispApp := dispD.windows[len(dispD.windows)-1].App.(*DisplayApp)
	dispApp.bg = 3
	dc := ui.NewCanvas(1024, 768)
	dispD.Draw(dc)
	savePreview(t, dc, "desk-preview-display-bg.png")

	dispApp.tab = 1
	dispApp.style = 2
	dispApp.wait = 3
	dispD.Draw(dc)
	savePreview(t, dc, "desk-preview-display-saver.png")

	dispApp.comboOpen = 1
	dispD.Draw(dc)
	savePreview(t, dc, "desk-preview-display-combo.png")
	dispApp.comboOpen = 0

	runD := New(1024, 768, ModeDesktop, Host{Info: info})
	runD.runHistory = []string{"notepad /root/文档/工作计划.txt", "calc", "find"}
	runD.openRunDialog()
	runApp := runD.windows[len(runD.windows)-1].App.(*RunDialogApp)
	runApp.setValue("notepad /root/文档/工作计划.txt")
	rc := ui.NewCanvas(1024, 768)
	runD.Draw(rc)
	savePreview(t, rc, "desk-preview-run.png")

	findD := New(1024, 768, ModeDesktop, Host{Info: info})
	findD.openFind()
	findApp := findD.windows[len(findD.windows)-1].App.(*FindApp)
	findApp.query = []rune("工作")
	findApp.queryCaret = len(findApp.query)
	findApp.done = true
	findApp.sel = 0
	findApp.status = "找到 4 个项目。"
	findApp.results = []findResult{
		{name: "工作计划.txt", dir: "/root/文档", path: "/root/文档/工作计划.txt", size: 184},
		{name: "工作日志.md", dir: "/root/文档", path: "/root/文档/工作日志.md", size: 2048},
		{name: "工作备份", dir: "/root/文档", path: "/root/文档/工作备份", isDir: true},
		{name: "工作台说明.html", dir: "/root/下载/示例", path: "/root/下载/示例/工作台说明.html", size: 3612},
	}
	fc := ui.NewCanvas(1024, 768)
	findD.Draw(fc)
	savePreview(t, fc, "desk-preview-find.png")

	flySaverD := New(1024, 768, ModeDesktop, Host{Info: info})
	flySaverD.saverStyle = saverFlying
	flySaverD.startSaver(now)
	for i := 1; i <= 90; i++ {
		flySaverD.stepSaver(now.Add(time.Duration(i) * 33 * time.Millisecond))
	}
	flc := ui.NewCanvas(1024, 768)
	flySaverD.Draw(flc)
	savePreview(t, flc, "desk-preview-saver-flying.png")

	renderApp(t, "desk-preview-solitaire.png",
		func(d *Desktop) App { return &SolitaireApp{d: d} },
		func(d *Desktop, app App) {
			a := app.(*SolitaireApp)
			a.newGame()
			a.stock = a.stock[:12]
			a.waste = []solCard{
				{suit: solDiamonds, rank: 10, up: true},
				{suit: solClubs, rank: 9, up: true},
				{suit: solHearts, rank: 9, up: true},
			}
			a.found[0] = []solCard{
				{suit: solHearts, rank: 1, up: true},
				{suit: solHearts, rank: 2, up: true},
				{suit: solHearts, rank: 3, up: true},
			}
			a.found[1] = []solCard{
				{suit: solSpades, rank: 1, up: true},
				{suit: solSpades, rank: 2, up: true},
			}
			a.tab[0] = []solCard{
				{suit: solSpades, rank: 13, up: true},
				{suit: solHearts, rank: 12, up: true},
				{suit: solClubs, rank: 11, up: true},
				{suit: solDiamonds, rank: 10, up: true},
			}
			a.tab[1] = []solCard{
				{suit: solClubs, rank: 13},
				{suit: solDiamonds, rank: 12, up: true},
				{suit: solSpades, rank: 11, up: true},
				{suit: solHearts, rank: 10, up: true},
			}
			a.tab[2] = []solCard{{suit: solDiamonds, rank: 7, up: true}}
			a.tab[3] = []solCard{
				{suit: solClubs, rank: 8},
				{suit: solClubs, rank: 7, up: true},
			}
			a.tab[4] = []solCard{
				{suit: solSpades, rank: 9, up: true},
				{suit: solHearts, rank: 8, up: true},
			}
			a.tab[5] = []solCard{
				{suit: solDiamonds, rank: 6},
				{suit: solDiamonds, rank: 5},
				{suit: solClubs, rank: 4, up: true},
			}
			a.tab[6] = []solCard{{suit: solHearts, rank: 3, up: true}}
			a.score, a.moves, a.elapsed = 185, 24, 93
		})
	renderApp(t, "desk-preview-downloader.png",
		func(d *Desktop) App {
			return &DownloaderApp{d: d, status: "输入下载地址，然后点“开始下载”。"}
		},
		func(d *Desktop, app App) {
			a := app.(*DownloaderApp)
			a.value = []rune("https://mirrors.tuna.tsinghua.edu.cn/alpine/v3.24/releases/aarch64/alpine-virt-3.24.3-aarch64.iso")
			a.caret = len(a.value)
		})
	renderApp(t, "desk-preview-downloader-progress.png",
		func(d *Desktop) App { return &DownloaderApp{d: d} },
		func(d *Desktop, app App) {
			a := app.(*DownloaderApp)
			a.value = []rune("https://mirrors.tuna.tsinghua.edu.cn/alpine/v3.24/releases/aarch64/alpine-virt-3.24.3-aarch64.iso")
			a.caret = len(a.value)
			a.state = dlWorking
			a.received = 23 * 1024 * 1024
			a.total = 64 * 1024 * 1024
			a.speed = 2.8 * 1024 * 1024
			a.dest = "alpine-virt-3.24.3-aarch64.iso"
			a.status = "正在下载 alpine-virt-3.24.3-aarch64.iso …"
		})
	renderApp(t, "desk-preview-nuke-auth.png",
		func(d *Desktop) App { return &NukeAuthApp{d: d} },
		func(d *Desktop, app App) {
			app.(*NukeAuthApp).code = []rune("00")
		})
	renderApp(t, "desk-preview-nuke-countdown.png",
		func(d *Desktop) App {
			return &NukeCountdownApp{d: d, deadline: time.Now().Add(6400 * time.Millisecond)}
		},
		nil)

	notifyD := New(1024, 768, ModeDesktop, Host{Info: info})
	notifyD.clock = "09:41"
	notifyD.cpuPct, notifyD.memPct = 37, 64
	notifyD.Notify("下载完成", "文件已保存：alpine-virt-3.24.3-aarch64.iso")
	nc := ui.NewCanvas(1024, 768)
	notifyD.Draw(nc)
	savePreview(t, nc, "desk-preview-notification.png")

	dragD := New(1024, 768, ModeDesktop, Host{Info: info})
	dragD.clock = "09:41"
	dragD.cpuPct, dragD.memPct = 37, 64
	dragD.fileDrag = &fileDrag{name: "季度报告.txt", path: "/root/文档/季度报告.txt"}
	for i, ic := range dragD.icons {
		if ic.key != "sys:recycle" {
			continue
		}
		ix, iy := dragD.iconPos(i)
		dragD.MouseX, dragD.MouseY = ix+43, iy+32
		break
	}
	dragCanvas := ui.NewCanvas(1024, 768)
	dragD.Draw(dragCanvas)
	savePreview(t, dragCanvas, "desk-preview-file-drag.png")

	overrideD := New(1024, 768, ModeDesktop, Host{Info: info})
	overrideD.nuke = true
	overrideD.nukeAt = time.Now()
	overrideD.nukeStep = overrideD.nukeAt
	oc := ui.NewCanvas(1024, 768)
	overrideD.Draw(oc)
	savePreview(t, oc, "desk-preview-nuke-override.png")

	nukeBSODD := New(1024, 768, ModeDesktop, Host{Info: info})
	nukeBSODD.showBSODText("System Override: Nuclear Strike Confirmed.")
	nbc := ui.NewCanvas(1024, 768)
	nukeBSODD.Draw(nbc)
	savePreview(t, nbc, "desk-preview-nuke-bsod.png")

	bsodD := New(1024, 768, ModeDesktop, Host{Info: info})
	bsodD.showBSOD()
	bsc := ui.NewCanvas(1024, 768)
	bsodD.Draw(bsc)
	savePreview(t, bsc, "desk-preview-bsod.png")

	sleepD := New(1024, 768, ModeDesktop, Host{Info: info})
	sleepD.sleeping = true
	slc := ui.NewCanvas(1024, 768)
	sleepD.Draw(slc)
	savePreview(t, slc, "desk-preview-sleep.png")

	fmt.Println("preview frames written")
}

// renderApp draws a single application window on an otherwise empty desktop.
// after runs once the window exists, for content that needs the canvas size.
func renderApp(t *testing.T, name string, makeApp func(d *Desktop) App, after func(d *Desktop, app App)) {
	t.Helper()
	d := New(1024, 768, ModeDesktop, Host{Info: "预览环境 · 1024×768"})
	app := makeApp(d)
	d.Open(app)
	if after != nil {
		after(d, app)
	}
	c := ui.NewCanvas(1024, 768)
	d.Draw(c)
	savePreview(t, c, name)
}
