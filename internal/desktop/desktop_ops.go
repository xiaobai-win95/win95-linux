package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"win95linux/internal/ui"
)

func (d *Desktop) selectedIcon() int {
	if d.selected >= 0 && d.selected < len(d.icons) {
		return d.selected
	}
	return -1
}

func (d *Desktop) selectIconPath(path string) {
	for i, ic := range d.icons {
		if ic.path == path {
			d.selected = i
			d.lastIcon = i
			d.lastClick = time.Time{}
			d.Invalidate()
			return
		}
	}
}

func (d *Desktop) activateIcon(idx int) {
	if idx < 0 || idx >= len(d.icons) {
		return
	}
	if a := d.icons[idx].action; a != nil {
		a()
	}
}

func (d *Desktop) openDesktopPath(path string) {
	st, err := os.Stat(path)
	if err != nil {
		d.MessageBox("桌面", "找不到“"+filepath.Base(path)+"”。\n\n它可能已被移动或删除。", IconWarning, []string{"确定"}, nil)
		d.refreshShortcuts(true)
		return
	}
	if st.IsDir() {
		d.openFiles(path)
		return
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".desktop") {
		if e, ok := parseShortcut(path); ok {
			if execLine := cleanExec(e.Exec); execLine != "" {
				d.launchShortcut(execLine, e.Terminal)
				return
			}
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".log", ".conf", ".ini", ".cfg":
		d.openNotepad(path)
	case ".html", ".htm":
		d.openBrowser("file://" + path)
	default:
		d.MessageBox("桌面", "无法打开“"+filepath.Base(path)+"”。\n\n没有与此文件类型关联的程序。", IconInfo, []string{"确定"}, nil)
	}
}

func (d *Desktop) rightClick(x, y int) {
	d.lastInput = time.Now()
	d.closeContext()
	d.closeMenusExcept(nil)
	if d.startOpen {
		d.startOpen = false
		d.startSub = ""
		d.startSel = ""
		d.startHover = ""
	}
	d.dragIcon = -1
	if y >= d.taskbarTop() {
		d.rightClickTaskbar(x, y)
		return
	}
	if w := d.windowAt(x, y); w != nil {
		d.bringToFront(w)
		if w.TitleRect().Contains(x, y) {
			d.openContext(d.windowMenuItems(w), x, y)
		}
		return
	}
	if idx := d.iconAt(x, y); idx >= 0 {
		d.selected = idx
		d.lastIcon = idx
		d.lastClick = time.Now()
		d.Invalidate()
		d.openContext(d.iconMenuItems(idx), x, y)
		return
	}
	d.selected = -1
	d.active = nil
	d.Invalidate()
	d.openContext(d.desktopMenuItems(), x, y)
}

func (d *Desktop) rightClickTaskbar(x, y int) {
	top := d.taskbarTop()
	start := ui.Rect{4, top + 4, startBtnW - 2, 26}
	if start.Contains(x, y) {
		d.openContext(d.startMenuButtonItems(), x, y)
		return
	}
	if w := d.taskButtonAt(x, y); w != nil {
		d.openContext(d.taskMenuItems(w), x, y)
		return
	}
	if d.clockRect().Contains(x, y) {
		d.openContext(d.clockMenuItems(), x, y)
		return
	}
	if d.sysTrayRect().Contains(x, y) {
		d.openContext(d.trayMenuItems(), x, y)
		return
	}
	d.openContext(d.taskbarMenuItems(), x, y)
}

func (d *Desktop) desktopMenuItems() []ctxItem {
	autoLabel := "自动排列(A)"
	return []ctxItem{
		{label: "排列图标(I)", icon: ui.IcoFolderOpen, enabled: true, children: []ctxItem{
			{label: autoLabel, checked: d.autoArrange, enabled: true, action: func() { d.toggleAutoArrange() }},
			{label: "对齐到网格(G)", enabled: !d.autoArrange, action: func() { d.alignIcons() }},
			{sep: true},
			{label: "按名称(N)", enabled: true, action: func() { d.sortDesktopIcons(false) }},
			{label: "按类型(T)", enabled: true, action: func() { d.sortDesktopIcons(true) }},
		}},
		{label: "刷新(E)", accel: "F5", icon: ui.IcoInfo, enabled: true, action: func() { d.refreshDesktop() }},
		{sep: true},
		{label: "新建(W)", icon: ui.IcoFolder, enabled: true, children: []ctxItem{
			{label: "文件夹(F)", icon: ui.IcoFolder, enabled: true, action: func() { d.newDesktopFolder() }},
			{label: "文本文档(T)", icon: ui.IcoText, enabled: true, action: func() { d.newDesktopText() }},
		}},
		{sep: true},
		{label: "属性(R)", icon: ui.IcoInfo, enabled: true, action: func() { d.showDesktopProperties() }},
	}
}

func (d *Desktop) iconMenuItems(idx int) []ctxItem {
	if idx < 0 || idx >= len(d.icons) {
		return nil
	}
	ic := d.icons[idx]
	user := ic.path != ""
	explore := ic.path != "" || ic.key == "sys:computer" || ic.key == "sys:docs" || ic.key == "sys:recycle"
	return []ctxItem{
		{label: "打开(O)", enabled: true, action: func() { d.activateIcon(idx) }},
		{label: "资源管理器(X)", enabled: explore, action: func() { d.exploreIcon(idx) }},
		{sep: true},
		{label: "重命名(M)", enabled: user, action: func() { d.renameIcon(idx) }},
		{label: "删除(D)", accel: "Del", enabled: user, action: func() { d.deleteIcon(idx) }},
		{sep: true},
		{label: "属性(R)", action: func() { d.showIconProperties(idx) }},
	}
}

func (d *Desktop) windowMenuItems(w *Window) []ctxItem {
	return []ctxItem{
		{label: "还原(R)", enabled: w.Maximized || w.Minimized, action: func() { d.restoreWindow(w) }},
		{label: "移动(M)", enabled: false},
		{label: "大小(S)", enabled: false},
		{sep: true},
		{label: "最小化(N)", enabled: !w.Minimized, action: func() { d.minimize(w) }},
		{label: "最大化(X)", enabled: w.Resizable && !w.Maximized, action: func() { d.maximizeWindow(w) }},
		{sep: true},
		{label: "关闭(C)", accel: "Alt+F4", enabled: true, action: func() { d.CloseWindow(w) }},
	}
}

func (d *Desktop) taskMenuItems(w *Window) []ctxItem {
	return []ctxItem{
		{label: "还原(R)", enabled: w.Minimized || w != d.active, action: func() { d.restoreWindow(w) }},
		{label: "最小化(N)", enabled: !w.Minimized, action: func() { d.minimize(w) }},
		{label: "最大化(X)", enabled: w.Resizable && !w.Maximized, action: func() { d.maximizeWindow(w) }},
		{sep: true},
		{label: "关闭(C)", enabled: true, action: func() { d.CloseWindow(w) }},
	}
}

func (d *Desktop) startMenuButtonItems() []ctxItem {
	return []ctxItem{
		{label: "打开开始菜单(O)", enabled: true, action: func() {
			d.startOpen = true
			d.startSub = ""
			d.startSel = ""
			d.startHover = ""
			d.Invalidate()
		}},
		{label: "资源管理器(X)", enabled: true, action: func() { d.openFiles("/root") }},
		{sep: true},
		{label: "关于本机(A)", enabled: true, action: func() { d.showAbout() }},
	}
}

func (d *Desktop) clockMenuItems() []ctxItem {
	return []ctxItem{
		{label: "查看日期和时间(D)", enabled: true, action: func() {
			d.MessageBox("日期/时间", d.statusClock(), IconInfo, []string{"确定"}, nil)
		}},
		{label: "系统监视器(M)", enabled: true, action: func() { d.showSysMonitor() }},
		{sep: true},
		{label: "关于本机(A)", enabled: true, action: func() { d.showAbout() }},
	}
}

func (d *Desktop) trayMenuItems() []ctxItem {
	return []ctxItem{
		{label: "系统监视器(M)", enabled: true, action: func() { d.showSysMonitor() }},
		{label: "桌面属性(R)", enabled: true, action: func() { d.showDesktopProperties() }},
		{sep: true},
		{label: "关于本机(A)", enabled: true, action: func() { d.showAbout() }},
	}
}

func (d *Desktop) taskbarMenuItems() []ctxItem {
	hasWin := false
	for _, w := range d.windows {
		if !w.Modal {
			hasWin = true
			break
		}
	}
	return []ctxItem{
		{label: "层叠窗口(C)", enabled: hasWin, action: func() { d.cascadeWindows() }},
		{label: "横向平铺(H)", enabled: hasWin, action: func() { d.tileWindows(true) }},
		{label: "纵向平铺(V)", enabled: hasWin, action: func() { d.tileWindows(false) }},
		{sep: true},
		{label: "最小化所有窗口(M)", enabled: hasWin, action: func() { d.minimizeAll() }},
		{sep: true},
		{label: "桌面属性(R)", enabled: true, action: func() { d.showDesktopProperties() }},
	}
}

func (d *Desktop) refreshDesktop() {
	d.closeContext()
	d.closeMenusExcept(nil)
	d.lastIcon = -1
	d.lastClick = time.Time{}
	d.refreshShortcuts(true)
	d.Invalidate()
}

func (d *Desktop) exploreIcon(idx int) {
	if idx < 0 || idx >= len(d.icons) {
		return
	}
	ic := d.icons[idx]
	switch {
	case ic.path != "":
		p := ic.path
		if !ic.isDir {
			p = filepath.Dir(p)
		}
		d.openFiles(p)
	case ic.key == "sys:computer":
		d.openFiles("")
	case ic.key == "sys:docs":
		d.openFiles("/root/文档")
	case ic.key == "sys:recycle":
		d.openRecycle()
	}
}

func uniqueDesktopName(dir, base, ext string) string {
	name := base + ext
	for i := 2; i < 10000; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	return name
}

func (d *Desktop) newDesktopFolder() {
	d.closeContext()
	if err := os.MkdirAll(shortcutsDir, 0o755); err != nil {
		d.MessageBox("桌面", "无法访问桌面文件夹：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	name := uniqueDesktopName(shortcutsDir, "新建文件夹", "")
	path := filepath.Join(shortcutsDir, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		d.MessageBox("桌面", "无法新建文件夹：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	d.refreshShortcuts(true)
	d.selectIconPath(path)
}

func (d *Desktop) newDesktopText() {
	d.closeContext()
	if err := os.MkdirAll(shortcutsDir, 0o755); err != nil {
		d.MessageBox("桌面", "无法访问桌面文件夹：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	name := uniqueDesktopName(shortcutsDir, "新建文本文档", ".txt")
	path := filepath.Join(shortcutsDir, name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		d.MessageBox("桌面", "无法新建文本文档：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	d.refreshShortcuts(true)
	d.selectIconPath(path)
}

func (d *Desktop) renameIcon(idx int) {
	d.closeContext()
	if idx < 0 || idx >= len(d.icons) {
		return
	}
	ic := d.icons[idx]
	if ic.path == "" {
		return
	}
	oldPath := ic.path
	d.InputBox("重命名", "输入新名称：", filepath.Base(oldPath), func(name string, ok bool) {
		if !ok {
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			d.MessageBox("重命名", "名称不能为空。", IconWarning, []string{"确定"}, nil)
			return
		}
		target := filepath.Join(filepath.Dir(oldPath), name)
		if target == oldPath {
			return
		}
		if _, err := os.Lstat(target); err == nil {
			d.MessageBox("重命名", "已经存在名为“"+name+"”的项目。\n\n请换一个名称。", IconWarning, []string{"确定"}, nil)
			return
		}
		if err := os.Rename(oldPath, target); err != nil {
			d.MessageBox("重命名", "无法重命名：\n"+err.Error(), IconError, []string{"确定"}, nil)
			return
		}
		d.refreshShortcuts(true)
		d.selectIconPath(target)
	})
}

func moveToTrash(path string) (string, error) {
	trash := "/root/.trash"
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(path)
	dest := filepath.Join(trash, base)
	for i := 1; i < 10000; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		if i == 1 {
			dest = filepath.Join(trash, fmt.Sprintf("%s-%d", base, time.Now().Unix()))
		} else {
			dest = filepath.Join(trash, fmt.Sprintf("%s-%d-%d", base, time.Now().Unix(), i))
		}
	}
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (d *Desktop) deleteIcon(idx int) {
	d.closeContext()
	if idx < 0 || idx >= len(d.icons) {
		return
	}
	ic := d.icons[idx]
	if ic.path == "" {
		return
	}
	path := ic.path
	d.MessageBox("确认删除", "确实要把“"+ic.label+"”放入回收站吗？", IconQuestion,
		[]string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			if _, err := moveToTrash(path); err != nil {
				d.MessageBox("桌面", "删除失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
				return
			}
			d.refreshShortcuts(true)
			d.selected = -1
			d.Invalidate()
		})
}

func (d *Desktop) showDesktopProperties() {
	auto := "关闭"
	if d.autoArrange {
		auto = "开启"
	}
	text := fmt.Sprintf("Windows 95 Linux 中文版\n\n桌面分辨率：%d × %d\n桌面文件夹：%s\n图标数量：%d\n自动排列：%s\n\n%s",
		d.W, d.H, shortcutsDir, len(d.icons), auto, d.Host.Info)
	d.MessageBox("显示 属性", text, IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) showIconProperties(idx int) {
	d.closeContext()
	if idx < 0 || idx >= len(d.icons) {
		return
	}
	ic := d.icons[idx]
	var b strings.Builder
	fmt.Fprintf(&b, "名称：%s\n", ic.label)
	switch ic.key {
	case "sys:computer":
		b.WriteString("类型：系统对象\n说明：浏览磁盘、用户文件夹和程序。")
	case "sys:docs":
		b.WriteString("类型：系统文件夹\n位置：/root/文档")
	case "sys:browser":
		b.WriteString("类型：程序\n说明：打开网络浏览器。")
	case "sys:xfce":
		b.WriteString("类型：程序\n说明：切换到 XFCE 图形桌面。")
	case "sys:recycle":
		b.WriteString("类型：系统文件夹\n位置：/root/.trash")
	case "sys:setup":
		b.WriteString("类型：程序\n说明：打开安装向导。")
	case "sys:setuphelp":
		b.WriteString("类型：说明文档")
	default:
		if ic.path != "" {
			typ := "文件"
			if ic.isDir {
				typ = "文件夹"
			}
			if !ic.isDir && strings.HasSuffix(strings.ToLower(ic.path), ".desktop") {
				typ = "快捷方式"
			}
			fmt.Fprintf(&b, "类型：%s\n位置：%s\n", typ, filepath.Dir(ic.path))
			if st, err := os.Stat(ic.path); err == nil {
				if !ic.isDir {
					fmt.Fprintf(&b, "大小：%s\n", sizeText(st.Size()))
				}
				fmt.Fprintf(&b, "修改时间：%s\n", st.ModTime().Format("2006-01-02 15:04"))
			}
			if !ic.isDir && strings.HasSuffix(strings.ToLower(ic.path), ".desktop") {
				if e, ok := parseShortcut(ic.path); ok {
					fmt.Fprintf(&b, "目标：%s", oneLine(cleanExec(e.Exec)))
				}
			}
		} else {
			b.WriteString("类型：桌面图标")
		}
	}
	d.MessageBox("属性 - "+ic.label, strings.TrimSpace(b.String()), IconInfo, []string{"确定"}, nil)
}

func (d *Desktop) gridIconPos(i int) (int, int) {
	const colW = 94
	const rowH = 84
	rows := (d.taskbarTop() - 16) / rowH
	if rows < 1 {
		rows = 1
	}
	return 16 + (i/rows)*colW, 14 + (i%rows)*rowH
}

func (d *Desktop) toggleAutoArrange() {
	d.closeContext()
	if d.autoArrange {
		positions := make(map[string]iconPoint, len(d.icons))
		for i, ic := range d.icons {
			x, y := d.gridIconPos(i)
			positions[ic.key] = iconPoint{x, y}
		}
		d.iconPositions = positions
		d.autoArrange = false
	} else {
		d.autoArrange = true
		d.iconPositions = map[string]iconPoint{}
	}
	d.Invalidate()
}

func snapToGrid(v, origin, step, max int) int {
	if v < origin {
		v = origin
	}
	n := (v - origin + step/2) / step
	out := origin + n*step
	if out > max {
		out = max
	}
	return out
}

func (d *Desktop) alignIcons() {
	d.closeContext()
	if d.autoArrange {
		return
	}
	used := map[iconPoint]bool{}
	maxX, maxY := d.W-88, d.taskbarTop()-78
	for i, ic := range d.icons {
		x, y := d.iconPos(i)
		x = snapToGrid(x, 16, 94, maxX)
		y = snapToGrid(y, 14, 84, maxY)
		for tries := 0; tries < 4096; tries++ {
			if !used[iconPoint{x, y}] {
				break
			}
			y += 84
			if y > maxY {
				x += 94
				y = 14
			}
			if x > maxX {
				x, y = 16, 14
			}
		}
		used[iconPoint{x, y}] = true
		d.iconPositions[ic.key] = iconPoint{x, y}
	}
	d.Invalidate()
}

func (d *Desktop) sortDesktopIcons(byType bool) {
	d.closeContext()
	nBase := len(d.baseIcons)
	if len(d.icons) <= nBase {
		return
	}
	selKey := ""
	if d.selected >= 0 && d.selected < len(d.icons) {
		selKey = d.icons[d.selected].key
	}
	dyn := d.icons[nBase:]
	sort.SliceStable(dyn, func(i, j int) bool {
		if byType {
			if dyn[i].isDir != dyn[j].isDir {
				return dyn[i].isDir
			}
			if dyn[i].kind != dyn[j].kind {
				return dyn[i].kind < dyn[j].kind
			}
		}
		return strings.ToLower(dyn[i].label) < strings.ToLower(dyn[j].label)
	})
	d.iconPositions = map[string]iconPoint{}
	d.selected = -1
	if selKey != "" {
		for i, ic := range d.icons {
			if ic.key == selKey {
				d.selected = i
				break
			}
		}
	}
	d.Invalidate()
}

func (d *Desktop) desktopKey(ev KeyEvent) {
	switch ev.Name {
	case "F5":
		d.refreshDesktop()
		return
	case "F2":
		if idx := d.selectedIcon(); idx >= 0 {
			d.renameIcon(idx)
		}
		return
	case "Delete":
		if idx := d.selectedIcon(); idx >= 0 {
			d.deleteIcon(idx)
		}
		return
	case "Enter":
		if idx := d.selectedIcon(); idx >= 0 {
			d.activateIcon(idx)
		}
		return
	case "Esc":
		if d.selected >= 0 {
			d.selected = -1
			d.Invalidate()
		}
		return
	case "Up", "Down", "Left", "Right":
		d.moveIconSelection(ev.Name)
		return
	}
	if ev.Ctrl && ev.Name == "N" {
		d.newDesktopFolder()
	}
}

func (d *Desktop) moveIconSelection(name string) {
	n := len(d.icons)
	if n == 0 {
		return
	}
	rows := (d.taskbarTop() - 16) / 84
	if rows < 1 {
		rows = 1
	}
	i := d.selected
	if i < 0 || i >= n {
		d.selected = 0
		d.Invalidate()
		return
	}
	switch name {
	case "Up":
		i--
	case "Down":
		i++
	case "Left":
		i -= rows
	case "Right":
		i += rows
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	if i != d.selected {
		d.selected = i
		d.Invalidate()
	}
}

func (d *Desktop) restoreWindow(w *Window) {
	if w == nil {
		return
	}
	if w.Minimized {
		d.bringToFront(w)
	}
	if w.Maximized {
		d.toggleMax(w)
	}
}

func (d *Desktop) maximizeWindow(w *Window) {
	if w == nil || !w.Resizable {
		return
	}
	if w.Minimized {
		d.bringToFront(w)
	}
	if !w.Maximized {
		d.bringToFront(w)
		d.toggleMax(w)
		return
	}
	d.bringToFront(w)
}

func (d *Desktop) arrangeableWindows() []*Window {
	var wins []*Window
	for _, w := range d.windows {
		if !w.Modal {
			wins = append(wins, w)
		}
	}
	return wins
}

func (d *Desktop) minimizeAll() {
	d.closeContext()
	for _, w := range d.windows {
		if w.Modal {
			continue
		}
		w.Minimized = true
		w.menuOpen = -1
	}
	d.active = nil
	d.Invalidate()
}

func (d *Desktop) cascadeWindows() {
	d.closeContext()
	wins := d.arrangeableWindows()
	if len(wins) == 0 {
		return
	}
	const margin, step = 12, 26
	baseW := d.W * 3 / 5
	baseH := d.taskbarTop() * 3 / 5
	maxW := d.W - 2*margin
	maxH := d.taskbarTop() - 2*margin
	for i, w := range wins {
		ww, hh := baseW, baseH
		if ww < w.MinW {
			ww = w.MinW
		}
		if hh < w.MinH {
			hh = w.MinH
		}
		if ww > maxW {
			ww = maxW
		}
		if hh > maxH {
			hh = maxH
		}
		x := margin + i*step
		y := margin + i*step
		if x+ww > d.W-margin {
			x = d.W - margin - ww
		}
		if y+hh > d.taskbarTop()-margin {
			y = d.taskbarTop() - margin - hh
		}
		if x < 2 {
			x = 2
		}
		if y < 2 {
			y = 2
		}
		w.Minimized = false
		w.Maximized = false
		w.drag = false
		w.resizing = false
		w.X, w.Y, w.W, w.H = x, y, ww, hh
		w.App.Resized(d, w, w.ClientRect())
	}
	d.Invalidate()
}

func (d *Desktop) tileWindows(horizontal bool) {
	d.closeContext()
	wins := d.arrangeableWindows()
	n := len(wins)
	if n == 0 {
		return
	}
	const margin = 4
	availW := d.W - 2*margin
	availH := d.taskbarTop() - 2*margin
	for i, w := range wins {
		x, y, ww, hh := margin, margin, availW, availH
		if horizontal {
			hh = availH / n
			y = margin + i*hh
		} else {
			ww = availW / n
			x = margin + i*ww
		}
		if ww < w.MinW {
			ww = w.MinW
		}
		if hh < w.MinH {
			hh = w.MinH
		}
		w.Minimized = false
		w.Maximized = false
		w.drag = false
		w.resizing = false
		w.X, w.Y, w.W, w.H = x, y, ww-2, hh-2
		w.App.Resized(d, w, w.ClientRect())
	}
	d.Invalidate()
}

func (d *Desktop) ordinaryWindowCount() int {
	n := 0
	for _, w := range d.windows {
		if !w.Modal {
			n++
		}
	}
	return n
}

type taskButton struct {
	w *Window
	r ui.Rect
}

func (d *Desktop) taskbarButtons() []taskButton {
	var wins []*Window
	for _, w := range d.windows {
		if !w.Modal {
			wins = append(wins, w)
		}
	}
	sort.SliceStable(wins, func(i, j int) bool { return wins[i].created.Before(wins[j].created) })
	top := d.taskbarTop()
	tray := d.sysTrayRect()
	avail := tray.X - 6 - 96
	n := len(wins)
	bw := 160
	if n > 0 {
		bw = avail / n
		if bw > 160 {
			bw = 160
		}
		if bw < 74 {
			bw = 74
		}
	}
	var out []taskButton
	x := 96
	for _, w := range wins {
		if x+bw > tray.X-2 {
			break
		}
		out = append(out, taskButton{w: w, r: ui.Rect{x, top + 4, bw - 2, 26}})
		x += bw
	}
	return out
}

func (d *Desktop) taskButtonAt(x, y int) *Window {
	for _, tb := range d.taskbarButtons() {
		if tb.r.Contains(x, y) {
			return tb.w
		}
	}
	return nil
}
