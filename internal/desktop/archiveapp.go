package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/archive"
	"win95linux/internal/ui"
)

// ArchiveApp is the Win95-style front end for the safe archive backend.
// Extraction and creation run on worker goroutines; all UI state is updated
// through Desktop.Post on the event-loop thread.
type ArchiveApp struct {
	Base
	d   *Desktop
	win *Window

	path    string
	format  archive.Format
	entries []archive.Entry
	sel     int
	scroll  VScroll

	err       string
	status    string
	busy      bool
	operation string
	done      int64
	total     int64
	current   string
	loadSeq   uint64
	closed    bool

	press                           string
	btnOpen, btnNew, btnExtract     ui.Rect
	btnClose, pathR, listR, statusR ui.Rect
	lastClick                       time.Time
	lastSel                         int
}

func NewArchiveApp(d *Desktop) *ArchiveApp {
	return &ArchiveApp{d: d, sel: -1, lastSel: -1}
}

func (d *Desktop) openArchiveManager(path string) {
	for _, w := range d.windows {
		if a, ok := w.App.(*ArchiveApp); ok && (path == "" || a.path == path) {
			d.bringToFront(w)
			return
		}
	}
	a := NewArchiveApp(d)
	a.win = d.Open(a)
	if path != "" {
		a.openPath(path)
	}
}

func (a *ArchiveApp) Title() string {
	if a.path == "" {
		return a.d.tr("万能压缩")
	}
	return filepath.Base(a.path) + " - " + a.d.tr("万能压缩")
}

func (a *ArchiveApp) Icon() ui.IconKind       { return ui.IcoPackage }
func (a *ArchiveApp) DefaultSize() (int, int) { return 860, 560 }
func (a *ArchiveApp) MinSize() (int, int)     { return 640, 380 }
func (a *ArchiveApp) Resizable() bool         { return true }

func (a *ArchiveApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: d.tr("文件(F)"), Items: []MenuItem{
			{Label: d.tr("打开压缩包(O)…"), Accel: "Ctrl+O", Enabled: !a.busy, Action: func() { a.openDialog(d) }},
			{Label: d.tr("新建 ZIP(N)"), Enabled: !a.busy, Action: func() { a.startCreate(d, "zip") }},
			{Label: d.tr("新建 tar.gz(T)"), Enabled: !a.busy, Action: func() { a.startCreate(d, "tar.gz") }},
			{Separator: true},
			{Label: d.tr("解压到…(E)"), Enabled: a.canExtract(), Action: func() { a.startExtract(d) }},
			{Label: d.tr("刷新(R)"), Accel: "F5", Enabled: !a.busy && a.path != "", Action: func() { a.openPath(a.path) }},
			{Separator: true},
			{Label: d.tr("关闭(C)"), Action: func() { d.CloseWindow(w) }},
		}},
		{Label: d.tr("帮助(H)"), Items: []MenuItem{
			{Label: d.tr("关于万能压缩(A)"), Action: func() {
				d.MessageBox(d.tr("关于万能压缩"),
					d.tr("Windows 95 Linux 万能压缩 26.6\n\n可读取 ZIP、7z、RAR、TAR、TAR.GZ、TAR.XZ 和 TAR.ZST。\n可创建 ZIP 或 TAR.GZ。\n\n解压会拒绝路径穿越、链接、特殊文件和加密成员。\n覆盖文件前必须确认，系统目录始终禁止作为目标。"),
					IconInfo, []string{d.tr("确定")}, nil)
			}},
		}},
	}
}

func (a *ArchiveApp) openPath(path string) {
	if a.busy || a.closed {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		a.err = a.d.trf("无法解析压缩包路径：%s", err.Error())
		a.d.Invalidate()
		return
	}
	a.path = filepath.Clean(abs)
	a.format = archive.FormatUnknown
	a.entries = nil
	a.sel = -1
	a.scroll.Pos = 0
	a.err = ""
	a.status = a.d.tr("正在读取压缩包…")
	a.busy = true
	a.operation = "读取"
	a.done, a.total, a.current = 0, 0, ""
	a.loadSeq++
	seq := a.loadSeq
	a.d.Invalidate()

	go func() {
		format, err := archive.Identify(a.path)
		var entries []archive.Entry
		if err == nil {
			entries, err = archive.List(a.path)
		}
		a.d.Post(func() {
			if a.closed || seq != a.loadSeq {
				return
			}
			a.busy = false
			a.operation = ""
			if err != nil {
				a.format = archive.FormatUnknown
				a.entries = nil
				a.err = err.Error()
				a.status = a.d.tr("无法打开压缩包")
				a.d.Invalidate()
				a.d.MessageBox(a.d.tr("万能压缩"), a.d.trf("无法打开压缩包：\n%s", err.Error()), IconError, []string{a.d.tr("确定")}, nil)
				return
			}
			a.format = format
			a.entries = entries
			a.sel = -1
			a.scroll.Pos = 0
			a.scroll.Total = len(entries)
			a.status = a.d.trf("已读取 %d 个成员", len(entries))
			a.d.Invalidate()
		})
	}()
}

func (a *ArchiveApp) openDialog(d *Desktop) {
	if a.busy {
		return
	}
	d.FileDialog(FileOpen, defaultArchiveDir(a.path), "", func(path string, ok bool) {
		if ok && path != "" {
			a.openPath(path)
		}
	})
}

func (a *ArchiveApp) canExtract() bool {
	return !a.busy && a.path != "" && a.format != archive.FormatUnknown && a.err == ""
}

func (a *ArchiveApp) startExtract(d *Desktop) {
	if !a.canExtract() {
		return
	}
	if key, name := archiveExtractionProblemKey(a.entries); key != "" {
		d.MessageBox(d.tr("无法解压"), d.trf(key, name), IconWarning, []string{d.tr("确定")}, nil)
		return
	}
	d.InputBoxChecked(d.tr("解压到文件夹"), d.tr("请输入目标文件夹的绝对路径："), defaultExtractDir(),
		func(input string) string {
			_, err := prepareArchiveDestination(input)
			if err != nil {
				return storageErrorText(d, err)
			}
			return ""
		}, func(input string, ok bool) {
			if !ok {
				return
			}
			dest, err := prepareArchiveDestination(input)
			if err != nil {
				d.MessageBox(d.tr("无法解压"), storageErrorText(d, err), IconError, []string{d.tr("确定")}, nil)
				return
			}
			conflicts, err := archiveExistingEntries(dest, a.entries)
			if err != nil {
				d.MessageBox(d.tr("无法检查目标目录"), storageErrorText(d, err), IconError, []string{d.tr("确定")}, nil)
				return
			}
			text := d.trf("即将解压：\n%s\n\n目标文件夹：\n%s\n\n成员：%d 个，文件总大小：%s\n",
				a.path, dest, len(a.entries), storageSizeText(d, archiveEntriesSize(a.entries)))
			if conflicts > 0 {
				text += d.trf("检测到 %d 个同名项目，确认后将被替换。\n", conflicts)
			}
			text += d.tr("\n系统目录已被禁止。确实要解压吗？")
			d.MessageBox(d.tr("确认解压"), text, IconWarning, []string{d.tr("解压"), d.tr("取消")}, func(i int) {
				if i == 0 {
					a.runExtract(dest)
				}
			})
		})
}

func (a *ArchiveApp) runExtract(dest string) {
	if a.busy {
		return
	}
	a.busy = true
	a.operation = "解压"
	a.err = ""
	a.status = a.d.tr("正在解压…")
	a.done, a.total, a.current = 0, archiveEntriesSize(a.entries), ""
	a.d.Invalidate()
	var lastReport time.Time
	progress := func(done, total int64, current string) {
		now := time.Now()
		if current != "" && now.Sub(lastReport) < 100*time.Millisecond {
			return
		}
		lastReport = now
		a.d.Post(func() {
			if a.closed {
				return
			}
			a.done, a.total, a.current = done, total, current
			a.status = a.d.tr("正在解压…")
			a.d.Invalidate()
		})
	}
	go func() {
		err := archive.Extract(a.path, dest, progress)
		a.d.Post(func() {
			if a.closed {
				return
			}
			a.busy = false
			a.operation = ""
			a.current = ""
			if err != nil {
				a.err = err.Error()
				a.status = a.d.tr("解压失败")
				a.d.Invalidate()
				a.d.MessageBox(a.d.tr("解压失败"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
				return
			}
			a.status = a.d.trf("解压完成：%s", dest)
			a.d.Invalidate()
			a.d.MessageBox(a.d.tr("解压完成"), a.d.trf("已解压到：\n%s", dest), IconInfo, []string{a.d.tr("确定")}, nil)
		})
	}()
}

func (a *ArchiveApp) startCreate(d *Desktop, format string) {
	if a.busy {
		return
	}
	ext := ".zip"
	if format == "tar.gz" {
		ext = ".tar.gz"
	}
	d.FileDialog(FileSave, defaultArchiveDir(""), d.tr("压缩包")+ext, func(path string, ok bool) {
		if !ok || path == "" {
			return
		}
		dest, overwrite, err := prepareArchiveOutput(path)
		if err != nil {
			d.MessageBox(d.tr("无法创建压缩包"), storageErrorText(d, err), IconError, []string{d.tr("确定")}, nil)
			return
		}
		a.askCreateSource(d, format, dest, overwrite)
	})
}

func (a *ArchiveApp) askCreateSource(d *Desktop, format, dest string, overwrite bool) {
	d.InputBoxChecked(d.tr("选择压缩来源"), d.tr("输入文件或文件夹的绝对路径，多个来源用分号分隔："), defaultArchiveDir(""),
		func(input string) string {
			_, err := archiveSourcePaths(input)
			if err != nil {
				return storageErrorText(d, err)
			}
			return ""
		}, func(input string, ok bool) {
			if !ok {
				return
			}
			sources, err := archiveSourcePaths(input)
			if err != nil {
				d.MessageBox(d.tr("无法创建压缩包"), storageErrorText(d, err), IconError, []string{d.tr("确定")}, nil)
				return
			}
			text := d.trf("将使用 %s 格式创建：\n%s\n\n来源：\n%s\n\n", strings.ToUpper(format), dest, strings.Join(sources, "\n"))
			icon := IconQuestion
			if overwrite {
				text += d.tr("目标文件已存在，确认后会被替换。\n")
				icon = IconWarning
			}
			text += d.tr("\n链接和特殊文件会被拒绝。确实要创建吗？")
			d.MessageBox(d.tr("确认创建压缩包"), text, icon, []string{d.tr("创建"), d.tr("取消")}, func(i int) {
				if i == 0 {
					a.runCreate(dest, sources, format)
				}
			})
		})
}

func (a *ArchiveApp) runCreate(dest string, sources []string, format string) {
	if a.busy {
		return
	}
	a.busy = true
	a.operation = "创建"
	a.err = ""
	a.status = a.d.tr("正在创建压缩包…")
	a.done, a.total, a.current = 0, 0, ""
	a.d.Invalidate()
	var lastReport time.Time
	progress := func(done, total int64, current string) {
		now := time.Now()
		if current != "" && now.Sub(lastReport) < 100*time.Millisecond {
			return
		}
		lastReport = now
		a.d.Post(func() {
			if a.closed {
				return
			}
			a.done, a.total, a.current = done, total, current
			a.status = a.d.tr("正在创建压缩包…")
			a.d.Invalidate()
		})
	}
	go func() {
		err := archive.Create(dest, sources, format, progress)
		a.d.Post(func() {
			if a.closed {
				return
			}
			a.busy = false
			a.operation = ""
			a.current = ""
			if err != nil {
				a.err = err.Error()
				a.status = a.d.tr("创建失败")
				a.d.Invalidate()
				a.d.MessageBox(a.d.tr("创建失败"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
				return
			}
			a.status = a.d.trf("已创建：%s", dest)
			a.d.Invalidate()
			a.d.MessageBox(a.d.tr("创建完成"), a.d.trf("已创建压缩包：\n%s", dest), IconInfo, []string{a.d.tr("确定")}, nil)
		})
	}()
}

func (a *ArchiveApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	tx, ty := r.X+5, r.Y+5
	for _, b := range []struct {
		label   string
		width   int
		rect    *ui.Rect
		id      string
		enabled bool
	}{
		{d.tr("打开"), 64, &a.btnOpen, "open", !a.busy},
		{d.tr("新建"), 64, &a.btnNew, "new", !a.busy},
		{d.tr("解压到…"), 86, &a.btnExtract, "extract", a.canExtract()},
		{d.tr("关闭"), 64, &a.btnClose, "close", true},
	} {
		*b.rect = ui.Rect{tx, ty, b.width, 24}
		ui.Button(c, *b.rect, b.label, a.press == b.id, b.enabled)
		tx += b.width + 5
	}
	c.HLine(r.X+4, r.Y+32, r.W-8, ui.ColShadow)
	c.HLine(r.X+4, r.Y+33, r.W-8, ui.ColWhite)

	a.pathR = ui.Rect{r.X + 5, r.Y + 38, r.W - 10, 25}
	c.FillRect(a.pathR, ui.ColField)
	c.Sunken(a.pathR)
	ui.DrawIcon16(c, ui.IcoPackage, a.pathR.X+5, a.pathR.Y+4)
	pathText := d.tr("打开一个压缩包，或新建 ZIP / tar.gz")
	if a.path != "" {
		pathText = d.trf("%s  ·  %d 个成员  ·  %s", d.tr(archiveFormatLabel(a.format)), len(a.entries), storageSizeText(d, archiveEntriesSize(a.entries)))
	}
	f.TextClip(c, a.pathR.X+26, a.pathR.Y+5, a.pathR.W-32, pathText, ui.ColText)

	a.statusR = ui.Rect{r.X, r.Bottom() - 29, r.W, 29}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	a.listR = ui.Rect{r.X + 5, a.pathR.Bottom() + 5, r.W - 10, a.statusR.Y - a.pathR.Bottom() - 10}
	if a.listR.H < 50 {
		a.listR.H = 50
	}
	c.FillRect(a.listR, ui.ColField)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	scrollW, headH, rowH := 16, 21, 22
	sizeW, typeW, timeW := 88, 104, 136
	nameW := inner.W - scrollW - sizeW - typeW - timeW - 8
	if nameW < 120 {
		nameW = 120
	}
	c.FillRect(ui.Rect{inner.X, inner.Y, inner.W - scrollW, headH}, ui.ColFace)
	heads := []struct {
		text string
		x    int
	}{{d.tr("名称"), inner.X + 5}, {d.tr("大小"), inner.X + nameW}, {d.tr("类型"), inner.X + nameW + sizeW}, {d.tr("修改时间"), inner.X + nameW + sizeW + typeW}}
	for _, h := range heads {
		f.Text(c, h.x, inner.Y+3, h.text, ui.ColText)
	}
	rows := (inner.H - headH - 3) / rowH
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.entries)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	if len(a.entries) == 0 {
		msg := d.tr("此压缩包没有成员。")
		if a.path == "" {
			msg = d.tr("请打开压缩包，或点击“新建”创建 ZIP / tar.gz。")
		}
		if a.err != "" {
			msg = a.err
		}
		f.TextClip(c, inner.X+10, inner.Y+headH+9, inner.W-30, msg, ui.ColShadow)
	}
	y := inner.Y + headH + 1
	for i := a.scroll.Pos; i < len(a.entries) && y+rowH <= inner.Bottom(); i++ {
		e := a.entries[i]
		rr := ui.Rect{inner.X + 1, y, inner.W - scrollW - 2, rowH - 1}
		col := ui.ColText
		if i == a.sel {
			c.FillRect(rr, ui.ColNavy)
			col = ui.ColWhite
		}
		kind := ui.IcoFile
		if e.Type == archive.TypeDir {
			kind = ui.IcoFolder
		}
		ui.DrawIcon16(c, kind, rr.X+3, rr.Y+2)
		f.TextClip(c, rr.X+24, rr.Y+3, nameW-27, e.Name, col)
		f.Text(c, inner.X+nameW, rr.Y+3, storageSizeText(d, e.Size), col)
		typ := d.tr(archiveEntryTypeLabel(e.Type))
		if e.Encrypted {
			typ += d.tr(" · 加密")
		}
		f.Text(c, inner.X+nameW+sizeW, rr.Y+3, typ, col)
		when := ""
		if !e.ModTime.IsZero() {
			when = e.ModTime.Format("2006-01-02 15:04")
		}
		f.Text(c, inner.X+nameW+sizeW+typeW, rr.Y+3, when, col)
		y += rowH
	}
	sr := ui.Rect{inner.Right() - scrollW - 1, inner.Y + headH + 1, scrollW, inner.H - headH - 2}
	a.scroll.Draw(c, sr)

	status := d.tr(a.status)
	statusColor := ui.ColText
	if a.busy {
		operation := d.tr(a.operation)
		if a.total > 0 {
			status = d.trf("%s：%s / %s  %s", operation, storageSizeText(d, a.done), storageSizeText(d, a.total), a.current)
		} else {
			status = d.trf("%s：%s", operation, a.current)
		}
	} else if a.err != "" {
		status = a.err
		statusColor = ui.ColRed
	} else if status == "" && a.sel >= 0 && a.sel < len(a.entries) {
		e := a.entries[a.sel]
		status = fmt.Sprintf("%s  ·  %s  ·  %s", e.Name, d.tr(archiveEntryTypeLabel(e.Type)), storageSizeText(d, e.Size))
		if e.LinkTarget != "" {
			status += "  →  " + e.LinkTarget
		}
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, status, statusColor)
}

func (a *ArchiveApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	inner := a.listR.Inset(2)
	sr := ui.Rect{inner.Right() - 17, inner.Y + 22, 16, inner.H - 22}
	if changed, consumed := a.scroll.Mouse(ev, sr); changed || consumed {
		if changed {
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseWheel {
		if a.listR.Contains(ev.X, ev.Y) {
			a.scroll.ScrollBy(-ev.Wheel)
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnNew.Contains(ev.X, ev.Y):
			a.press = "new"
		case a.btnExtract.Contains(ev.X, ev.Y):
			a.press = "extract"
		case a.btnClose.Contains(ev.X, ev.Y):
			a.press = "close"
		case a.listR.Contains(ev.X, ev.Y):
			if idx := a.archiveRowAt(ev.X, ev.Y); idx >= 0 {
				a.sel = idx
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastSel == idx {
					a.showEntry(d, idx)
				}
				a.lastClick = time.Now()
				a.lastSel = idx
				d.Invalidate()
			}
		}
		if a.press != "" {
			d.Invalidate()
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "open":
			if a.btnOpen.Contains(ev.X, ev.Y) && !a.busy {
				a.openDialog(d)
			}
		case "new":
			if a.btnNew.Contains(ev.X, ev.Y) && !a.busy {
				a.startCreate(d, "zip")
			}
		case "extract":
			if a.btnExtract.Contains(ev.X, ev.Y) {
				a.startExtract(d)
			}
		case "close":
			if a.btnClose.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
		d.Invalidate()
	}
}

func (a *ArchiveApp) archiveRowAt(x, y int) int {
	inner := a.listR.Inset(2)
	if y < inner.Y+22 {
		return -1
	}
	idx := a.scroll.Pos + (y-inner.Y-22)/22
	if idx >= 0 && idx < len(a.entries) {
		return idx
	}
	return -1
}

func (a *ArchiveApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl && !ev.Alt {
		switch strings.ToLower(string(ev.Rune)) {
		case "o":
			a.openDialog(d)
			return true
		case "n":
			a.startCreate(d, "zip")
			return true
		}
	}
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "F5":
		if a.path != "" {
			a.openPath(a.path)
		}
		return true
	case "Up":
		a.moveSelection(-1, d)
		return true
	case "Down":
		a.moveSelection(1, d)
		return true
	case "PgUp":
		a.moveSelection(-maxInt(1, a.scroll.Visible), d)
		return true
	case "PgDn":
		a.moveSelection(maxInt(1, a.scroll.Visible), d)
		return true
	case "Home":
		a.moveSelection(-len(a.entries), d)
		return true
	case "End":
		a.moveSelection(len(a.entries), d)
		return true
	case "Enter":
		a.showEntry(d, a.sel)
		return true
	}
	return false
}

func (a *ArchiveApp) moveSelection(delta int, d *Desktop) {
	if len(a.entries) == 0 {
		return
	}
	a.sel += delta
	if a.sel < 0 {
		a.sel = 0
	}
	if a.sel >= len(a.entries) {
		a.sel = len(a.entries) - 1
	}
	if a.sel < a.scroll.Pos {
		a.scroll.Pos = a.sel
	}
	if a.sel >= a.scroll.Pos+a.scroll.Visible {
		a.scroll.Pos = a.sel - a.scroll.Visible + 1
	}
	a.scroll.Clamp()
	d.Invalidate()
}

func (a *ArchiveApp) showEntry(d *Desktop, idx int) {
	if idx < 0 || idx >= len(a.entries) {
		return
	}
	e := a.entries[idx]
	when := d.tr("未记录")
	if !e.ModTime.IsZero() {
		when = e.ModTime.Format("2006-01-02 15:04:05")
	}
	text := d.trf("名称：%s\n类型：%s\n大小：%s\n修改时间：%s\n权限：%s\n",
		e.Name, d.tr(archiveEntryTypeLabel(e.Type)), storageSizeText(d, e.Size), when, e.Mode.String())
	if e.LinkTarget != "" {
		text += d.trf("链接目标：%s\n", e.LinkTarget)
	}
	if e.Encrypted {
		text += d.tr("状态：加密成员，不能解压\n")
	}
	d.MessageBox(d.tr("成员属性"), text, IconInfo, []string{d.tr("确定")}, nil)
}

func (a *ArchiveApp) RequestClose(d *Desktop, w *Window) bool {
	if !a.busy {
		return true
	}
	d.MessageBox(d.tr("万能压缩"), d.tr("压缩包操作正在进行，请等待完成后再关闭。"), IconInfo, []string{d.tr("确定")}, nil)
	return false
}

func (a *ArchiveApp) Close() { a.closed = true }

func defaultArchiveDir(path string) string {
	if path != "" {
		dir := filepath.Dir(path)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	for _, dir := range []string{"/root/桌面", "/root/文档", "/root/下载"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return "/root"
}

func defaultExtractDir() string {
	for _, dir := range []string{"/root/桌面", "/root/文档", "/root/下载"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return "/root/桌面"
}

func archiveFormatLabel(format archive.Format) string {
	switch format {
	case archive.FormatZIP:
		return "ZIP"
	case archive.Format7z:
		return "7z"
	case archive.FormatRAR:
		return "RAR"
	case archive.FormatTar:
		return "TAR"
	case archive.FormatTarGZ:
		return "TAR.GZ"
	case archive.FormatTarXZ:
		return "TAR.XZ"
	case archive.FormatTarZST:
		return "TAR.ZST"
	default:
		return "未知格式"
	}
}

func archiveEntryTypeLabel(typ archive.EntryType) string {
	switch typ {
	case archive.TypeFile:
		return "文件"
	case archive.TypeDir:
		return "文件夹"
	case archive.TypeSymlink:
		return "符号链接"
	case archive.TypeHardLink:
		return "硬链接"
	default:
		return "特殊文件"
	}
}

func archiveEntriesSize(entries []archive.Entry) int64 {
	var total int64
	for _, e := range entries {
		if e.Type == archive.TypeFile {
			total += e.Size
		}
	}
	return total
}

func storageSizeText(d *Desktop, n int64) string {
	if n < 1<<10 {
		return d.trf("%d 字节", n)
	}
	return formatBytes(n)
}

// storageUIError keeps the original Chinese error contract for helpers and
// callers while allowing UI display sites to translate only our own text.
// Wrapped backend errors remain available through errors.Is/errors.As.
type storageUIError struct {
	key   string
	args  []any
	cause error
}

func (e *storageUIError) Error() string { return fmt.Sprintf(e.key, e.args...) }
func (e *storageUIError) Unwrap() error { return e.cause }

func storageErrorf(key string, args ...any) error {
	return &storageUIError{key: key, args: args}
}

func storageWrapErrorf(key string, cause error, args ...any) error {
	args = append(args, cause.Error())
	return &storageUIError{key: key, args: args, cause: cause}
}

func storageErrorText(d *Desktop, err error) string {
	if err == nil {
		return ""
	}
	var uiErr *storageUIError
	if errors.As(err, &uiErr) {
		return d.trf(uiErr.key, uiErr.args...)
	}
	return err.Error()
}

func archiveExtractionProblem(entries []archive.Entry) string {
	key, name := archiveExtractionProblemKey(entries)
	if key == "" {
		return ""
	}
	return fmt.Sprintf(key, name)
}

func archiveExtractionProblemKey(entries []archive.Entry) (string, string) {
	for _, e := range entries {
		switch {
		case e.Encrypted:
			return "加密成员“%s”不受支持，已取消解压。", e.Name
		case e.Type == archive.TypeSymlink || e.Type == archive.TypeHardLink:
			return "链接成员“%s”不会被创建，已取消解压。", e.Name
		case e.Type != archive.TypeFile && e.Type != archive.TypeDir:
			return "特殊文件“%s”不会被创建，已取消解压。", e.Name
		}
	}
	return "", ""
}

func archiveSourcePaths(input string) ([]string, error) {
	var paths []string
	for _, part := range strings.Split(input, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.IndexByte(part, 0) >= 0 || !filepath.IsAbs(part) {
			return nil, storageErrorf("来源必须是绝对路径：%s", part)
		}
		if _, err := os.Lstat(part); err != nil {
			return nil, storageWrapErrorf("无法访问来源“%s”：%s", err, part)
		}
		paths = append(paths, filepath.Clean(part))
	}
	if len(paths) == 0 {
		return nil, storageErrorf("请至少选择一个文件或文件夹")
	}
	return paths, nil
}

// canonicalArchivePath resolves the deepest existing ancestor without creating
// anything. This lets validation catch symlinks into protected system paths.
func canonicalArchivePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", storageErrorf("路径不能为空或包含 NUL")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if info, err := os.Lstat(abs); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return "", storageErrorf("不能使用符号链接作为目标")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	current := abs
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if current != abs && !resolvedInfo.IsDir() {
				return "", storageErrorf("路径组件不是文件夹：%s", current)
			}
			if current == abs && !resolvedInfo.IsDir() && !resolvedInfo.Mode().IsRegular() {
				return "", storageErrorf("目标是特殊文件：%s", abs)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", storageErrorf("无法解析路径：%s", abs)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

var archiveSystemDirs = []string{
	"/", "/root", "/home", "/bin", "/sbin", "/etc", "/usr", "/lib", "/lib64",
	"/boot", "/dev", "/proc", "/sys", "/run", "/opt", "/var", "/var/lib", "/var/run",
	"/var/log", "/var/spool", "/var/cache",
	// Darwin aliases matter when tests or local development resolve /etc.
	"/private/etc", "/private/usr", "/private/bin", "/private/sbin", "/private/lib",
	"/private/var/db", "/private/var/root",
}

func archivePathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func archivePathProtected(path string) bool {
	path = filepath.Clean(path)
	for _, dir := range archiveSystemDirs {
		switch dir {
		case "/", "/root", "/home", "/var":
			// Root, home and /var themselves are protected, while ordinary
			// user folders below them remain valid destinations.
			if path == dir {
				return true
			}
		default:
			if archivePathWithin(dir, path) {
				return true
			}
		}
	}
	return false
}

func prepareArchiveDestination(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", storageErrorf("目标文件夹不能为空")
	}
	abs, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	canonical, err := canonicalArchivePath(abs)
	if err != nil {
		return "", err
	}
	if archivePathProtected(abs) || archivePathProtected(canonical) {
		return "", storageErrorf("系统目录受保护，不能作为解压目标")
	}
	if info, err := os.Stat(canonical); err == nil && !info.IsDir() {
		return "", storageErrorf("解压目标不是文件夹")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return canonical, nil
}

func prepareArchiveOutput(input string) (string, bool, error) {
	if strings.TrimSpace(input) == "" {
		return "", false, storageErrorf("压缩包目标不能为空")
	}
	abs, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", false, err
	}
	canonical, err := canonicalArchivePath(abs)
	if err != nil {
		return "", false, err
	}
	if archivePathProtected(abs) || archivePathProtected(canonical) {
		return "", false, storageErrorf("系统目录受保护，不能在其中创建压缩包")
	}
	overwrite := false
	if info, err := os.Stat(canonical); err == nil {
		if !info.Mode().IsRegular() {
			return "", false, storageErrorf("压缩包目标不是普通文件")
		}
		overwrite = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	parent, err := os.Stat(filepath.Dir(canonical))
	if err != nil || !parent.IsDir() {
		return "", false, storageErrorf("压缩包目标文件夹不存在")
	}
	return canonical, overwrite, nil
}

func archiveExistingEntries(dest string, entries []archive.Entry) (int, error) {
	if _, err := os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	conflicts := 0
	for _, e := range entries {
		if e.Name == "." {
			continue
		}
		_, err := root.Lstat(e.Name)
		if err == nil {
			conflicts++
		} else if !errors.Is(err, fs.ErrNotExist) {
			return 0, storageWrapErrorf("无法检查成员“%s”：%s", err, e.Name)
		}
	}
	return conflicts, nil
}
