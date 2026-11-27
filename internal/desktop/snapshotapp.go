package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"win95linux/internal/snapshot"
	"win95linux/internal/ui"
)

const (
	defaultSnapshotBaseDir = "/var/lib/win95/snapshots"
	maxSnapshotDescription = 4096
)

type snapshotManagerItem struct {
	snapshot    snapshot.Snapshot
	description string
}

// SnapshotManagerApp always requires an explicit snapshot selection and an
// explicit restore target. Restore is a two-dialog, warning-only flow.
type SnapshotManagerApp struct {
	Base
	d        *Desktop
	win      *Window
	baseDir  string
	items    []snapshotManagerItem
	sel      int
	chosen   bool
	scroll   VScroll
	busy     bool
	closed   bool
	status   string
	err      string
	progress string

	listFn    func(string) ([]snapshot.Snapshot, error)
	createFn  func(string, string, func(snapshot.Progress)) (snapshot.Snapshot, error)
	restoreFn func(string, string, string, func(snapshot.Progress)) error

	listR, scrollR                         ui.Rect
	rowRects                               []ui.Rect
	baseR, refreshR, createR, descriptionR ui.Rect
	restoreR, closeR                       ui.Rect
	press                                  string
}

func newSnapshotManagerApp(d *Desktop) *SnapshotManagerApp {
	return &SnapshotManagerApp{
		d: d, baseDir: defaultSnapshotBaseDir, sel: -1,
		status: d.tr("正在读取快照…"),
		listFn: snapshot.List, createFn: snapshot.Create, restoreFn: snapshot.Restore,
	}
}

// openSnapshotManager is the integration entry point; existing menus are not
// changed by this module.
func (d *Desktop) openSnapshotManager() {
	for _, w := range d.windows {
		if _, ok := w.App.(*SnapshotManagerApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := newSnapshotManagerApp(d)
	w := d.Open(a)
	a.win = w
	a.refresh()
}

func (a *SnapshotManagerApp) Title() string           { return a.d.tr("快照管理") }
func (a *SnapshotManagerApp) Icon() ui.IconKind       { return ui.IcoDrive }
func (a *SnapshotManagerApp) DefaultSize() (int, int) { return 840, 520 }
func (a *SnapshotManagerApp) MinSize() (int, int)     { return 720, 460 }
func (a *SnapshotManagerApp) Resizable() bool         { return true }

func (a *SnapshotManagerApp) RequestClose(d *Desktop, w *Window) bool {
	if a.busy {
		d.MessageBox(d.tr("快照管理"), d.tr("快照操作正在进行中，请等待完成后再关闭。"), IconWarning, []string{d.tr("确定")}, nil)
		return false
	}
	return true
}

func (a *SnapshotManagerApp) Close() { a.closed = true }

func (a *SnapshotManagerApp) post(f func()) {
	if a.d != nil {
		a.d.Post(f)
	}
}

func (a *SnapshotManagerApp) selected() *snapshotManagerItem {
	if !a.chosen || a.sel < 0 || a.sel >= len(a.items) {
		return nil
	}
	return &a.items[a.sel]
}

type snapshotListResult struct {
	items []snapshotManagerItem
	err   error
}

func (a *SnapshotManagerApp) refresh() {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.sel, a.chosen = -1, false
	a.status = a.d.tr("正在读取快照…")
	a.err = ""
	a.d.Invalidate()
	base := a.baseDir
	done := make(chan snapshotListResult, 1)
	go func() {
		snaps, err := a.listFn(base)
		if err != nil {
			done <- snapshotListResult{err: err}
			return
		}
		items := make([]snapshotManagerItem, 0, len(snaps))
		for _, snap := range snaps {
			description, _ := readSnapshotDescription(base, snap.ID)
			items = append(items, snapshotManagerItem{snapshot: snap, description: description})
		}
		done <- snapshotListResult{items: items}
	}()
	go func() {
		res := <-done
		a.post(func() {
			if a.closed {
				return
			}
			a.busy = false
			if res.err != nil {
				a.items = nil
				a.err = res.err.Error()
				a.status = a.d.tr("快照读取失败")
			} else {
				a.items = res.items
				a.err = ""
				a.status = a.d.trf("共 %d 个快照；默认未选择", len(a.items))
			}
			a.scroll.Total = len(a.items)
			a.scroll.Pos = 0
			a.d.Invalidate()
		})
	}()
}

func (a *SnapshotManagerApp) setBaseDir() {
	if a.busy {
		return
	}
	a.d.InputBoxChecked(a.d.tr("快照库位置"), a.d.tr("输入快照库的绝对路径："), a.baseDir, func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || !filepath.IsAbs(s) {
			return a.d.tr("请输入快照库的绝对路径。")
		}
		if info, err := os.Lstat(s); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return a.d.tr("快照库必须是真实目录，不能是符号链接。")
		}
		return ""
	}, func(path string, ok bool) {
		if !ok {
			return
		}
		a.baseDir = filepath.Clean(strings.TrimSpace(path))
		a.refresh()
	})
}

func (a *SnapshotManagerApp) createSelected() {
	if a.busy {
		return
	}
	a.d.InputBoxChecked(a.d.tr("创建快照"), a.d.tr("输入要保存的当前工作目录（绝对路径）："), "", func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || !filepath.IsAbs(s) {
			return a.d.tr("请输入当前工作目录的绝对路径。")
		}
		if _, err := validateSnapshotDirectory(s); err != nil {
			return err.Error()
		}
		return ""
	}, func(root string, ok bool) {
		if !ok {
			return
		}
		resolved, err := validateSnapshotDirectory(strings.TrimSpace(root))
		if err != nil {
			a.d.MessageBox(a.d.tr("创建快照"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		root = resolved
		a.d.InputBoxChecked(a.d.tr("快照说明"), a.d.tr("输入说明（可留空，最多 500 字）："), "", func(s string) string {
			if len([]rune(s)) > 500 || strings.ContainsAny(s, "\x00\r\n") {
				return a.d.tr("说明不能包含换行或空字符，且不能超过 500 字。")
			}
			return ""
		}, func(description string, ok bool) {
			if !ok {
				return
			}
			text := a.d.trf("即将为以下当前工作创建快照：\n\n工作目录：%s\n快照库：%s\n说明：%s\n\n快照会复制普通文件并记录符号链接；proc、sys、dev、run、tmp 及特殊文件会跳过。此操作不会改写当前工作。",
				root, a.baseDir, snapshotDescriptionText(a.d, description))
			a.d.MessageBox(a.d.tr("确认创建快照"), text, IconQuestion, []string{a.d.tr("创建快照"), a.d.tr("取消")}, func(i int) {
				if i == 0 {
					a.startCreate(root, strings.TrimSpace(description))
				}
			})
		})
	})
}

func snapshotDescriptionOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return strings.TrimSpace(s)
}

func snapshotDescriptionText(d *Desktop, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return d.tr("（无）")
	}
	return s
}

type snapshotCreateResult struct {
	snapshot snapshot.Snapshot
	err      error
}

func (a *SnapshotManagerApp) startCreate(root, description string) {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.status = a.d.tr("正在创建快照…")
	a.progress = a.d.tr("扫描中")
	a.err = ""
	a.d.Invalidate()
	base := a.baseDir
	progress := make(chan snapshot.Progress, 32)
	done := make(chan snapshotCreateResult, 1)
	go func() {
		resolvedRoot, validationErr := validateSnapshotDirectory(root)
		if validationErr != nil {
			done <- snapshotCreateResult{err: validationErr}
			return
		}
		snap, err := a.createFn(resolvedRoot, base, func(p snapshot.Progress) {
			select {
			case progress <- p:
			default:
			}
		})
		if err == nil && description != "" {
			if saveErr := writeSnapshotDescription(base, snap.ID, description); saveErr != nil {
				err = fmt.Errorf("快照已创建，但说明保存失败: %w", saveErr)
			}
		}
		done <- snapshotCreateResult{snapshot: snap, err: err}
	}()
	go func() {
		for {
			select {
			case p := <-progress:
				a.post(func() {
					if a.closed || !a.busy {
						return
					}
					a.progress = snapshotProgressTextForUI(a.d, p)
					a.d.Invalidate()
				})
			case res := <-done:
				a.post(func() {
					if a.closed {
						return
					}
					a.busy = false
					a.progress = ""
					if res.err != nil {
						a.err = res.err.Error()
						a.status = a.d.tr("创建快照失败")
						a.d.MessageBox(a.d.tr("创建快照"), res.err.Error(), IconError, []string{a.d.tr("确定")}, nil)
					} else {
						a.err = ""
						a.status = a.d.trf("快照已创建: %s", res.snapshot.ID)
						a.d.MessageBox(a.d.tr("快照已创建"), a.d.trf("快照 %s 已保存。\n当前工作未被改写。", res.snapshot.ID), IconInfo, []string{a.d.tr("确定")}, nil)
					}
					if res.snapshot.ID != "" {
						a.refresh()
					}
					a.d.Invalidate()
				})
				return
			}
		}
	}()
}

func snapshotProgressPhase(p snapshot.Progress) string {
	phase := map[string]string{
		snapshot.PhaseScanning: "扫描", snapshot.PhaseSkipped: "跳过", snapshot.PhaseCopying: "复制",
		snapshot.PhaseLinking: "复用", snapshot.PhaseRestoring: "恢复", snapshot.PhaseConflict: "冲突", snapshot.PhaseDone: "完成",
	}[p.Phase]
	if phase == "" {
		return p.Phase
	}
	return phase
}

func snapshotProgressText(p snapshot.Progress) string {
	phase := snapshotProgressPhase(p)
	if p.Total > 0 {
		return fmt.Sprintf("%s %d/%d：%s", phase, p.Done, p.Total, p.Path)
	}
	if p.Path != "" {
		return phase + "：" + p.Path
	}
	return phase
}

func snapshotProgressTextForUI(d *Desktop, p snapshot.Progress) string {
	phase := d.tr(snapshotProgressPhase(p))
	if p.Total > 0 {
		return d.trf("%s %d/%d：%s", phase, p.Done, p.Total, p.Path)
	}
	if p.Path != "" {
		return d.trf("%s：%s", phase, p.Path)
	}
	return phase
}

func (a *SnapshotManagerApp) editDescription() {
	item := a.selected()
	if item == nil || a.busy {
		return
	}
	a.d.InputBoxChecked(a.d.tr("编辑快照说明"), a.d.trf("快照 %s 的说明：", item.snapshot.ID), item.description, func(s string) string {
		if len([]rune(s)) > 500 || strings.ContainsAny(s, "\x00\r\n") {
			return a.d.tr("说明不能包含换行或空字符，且不能超过 500 字。")
		}
		return ""
	}, func(description string, ok bool) {
		if !ok {
			return
		}
		description = strings.TrimSpace(description)
		if err := writeSnapshotDescription(a.baseDir, item.snapshot.ID, description); err != nil {
			a.d.MessageBox(a.d.tr("保存说明失败"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		item.description = description
		a.status = a.d.tr("快照说明已保存")
		a.d.Invalidate()
	})
}

func (a *SnapshotManagerApp) restoreSelected() {
	item := a.selected()
	if item == nil || a.busy {
		return
	}
	a.d.InputBoxChecked(a.d.tr("恢复快照"), a.d.trf("快照 %s 的原工作目录：\n%s\n\n输入要恢复到的目标目录：", item.snapshot.ID, item.snapshot.Root), item.snapshot.Root, func(s string) string {
		root := strings.TrimSpace(s)
		if _, err := validateSnapshotRestoreRoot(root, a.baseDir, item.snapshot.ID); err != nil {
			return err.Error()
		}
		return ""
	}, func(root string, ok bool) {
		if !ok {
			return
		}
		resolved, err := validateSnapshotRestoreRoot(strings.TrimSpace(root), a.baseDir, item.snapshot.ID)
		if err != nil {
			a.d.MessageBox(a.d.tr("恢复快照"), err.Error(), IconError, []string{a.d.tr("确定")}, nil)
			return
		}
		root = resolved
		confirmSnapshotRestore(a.d, *item, root, a.baseDir, func() {
			a.startRestore(*item, root)
		}, func() {
			a.status = a.d.tr("已取消恢复，当前工作未被改写")
			a.d.Invalidate()
		})
	})
}

func confirmSnapshotRestore(d *Desktop, item snapshotManagerItem, root, base string, proceed func(), cancel func()) {
	text := d.trf("快照 ID：%s\n时间：%s\n大小：%s\n说明：%s\n\n恢复到：%s\n\n影响：快照中的文件会替换目标目录里的同名文件，符号链接和目录元数据也会恢复；快照之外的文件保留。当前工作可能丢失，且无法自动撤销。",
		item.snapshot.ID, item.snapshot.CreatedAt.Local().Format("2006-01-02 15:04:05"), diskSizeTextForUI(d, uint64SnapshotBytes(item.snapshot.Bytes)), snapshotDescriptionText(d, item.description), root)
	d.MessageBox(d.tr("危险：恢复快照"), text, IconWarning, []string{d.tr("继续"), d.tr("取消")}, func(i int) {
		if i != 0 {
			cancel()
			return
		}
		d.MessageBox(d.tr("最后确认：覆盖当前工作"), text+d.tr("\n\n这是第二次确认。恢复将替换快照内的现有文件；只有点击“确认覆盖并恢复”才会继续。"), IconWarning, []string{d.tr("确认覆盖并恢复"), d.tr("取消")}, func(j int) {
			if j != 0 {
				cancel()
				return
			}
			proceed()
		})
	})
}

func uint64SnapshotBytes(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}

type snapshotRestoreResult struct{ err error }

func (a *SnapshotManagerApp) startRestore(item snapshotManagerItem, root string) {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.status = a.d.tr("正在恢复快照…")
	a.progress = a.d.tr("校验中")
	a.err = ""
	a.d.Invalidate()
	base, id := a.baseDir, item.snapshot.ID
	progress := make(chan snapshot.Progress, 32)
	done := make(chan snapshotRestoreResult, 1)
	go func() {
		resolvedRoot, err := validateSnapshotRestoreRoot(root, base, id)
		if err != nil {
			done <- snapshotRestoreResult{err: err}
			return
		}
		err = a.restoreFn(base, id, resolvedRoot, func(p snapshot.Progress) {
			select {
			case progress <- p:
			default:
			}
		})
		done <- snapshotRestoreResult{err: err}
	}()
	go func() {
		for {
			select {
			case p := <-progress:
				a.post(func() {
					if a.closed || !a.busy {
						return
					}
					a.progress = snapshotProgressTextForUI(a.d, p)
					a.d.Invalidate()
				})
			case res := <-done:
				a.post(func() {
					if a.closed {
						return
					}
					a.busy = false
					a.progress = ""
					if res.err != nil {
						a.err = res.err.Error()
						a.status = a.d.tr("恢复快照失败")
						a.d.MessageBox(a.d.tr("恢复快照失败"), res.err.Error(), IconError, []string{a.d.tr("确定")}, nil)
					} else {
						a.err = ""
						a.status = a.d.trf("快照已恢复到 %s", root)
						a.d.MessageBox(a.d.tr("恢复完成"), a.d.trf("快照 %s 已恢复到 %s。", id, root), IconInfo, []string{a.d.tr("确定")}, nil)
					}
					a.d.Invalidate()
				})
				return
			}
		}
	}()
}

func validateSnapshotDirectory(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("工作目录必须是绝对路径")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("无法访问工作目录: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("工作目录必须是真实目录，不能是符号链接")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("解析工作目录失败: %w", err)
	}
	info, err = os.Lstat(resolved)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("解析后的工作目录不是真实目录")
	}
	resolved = filepath.Clean(resolved)
	if resolved == string(filepath.Separator) {
		return "", errors.New("不能使用文件系统根目录作为快照工作或恢复目录")
	}
	switch resolved {
	case "/proc", "/sys", "/dev", "/run", "/tmp":
		return "", errors.New("该目录是系统运行时目录，不能作为快照工作目录")
	}
	return resolved, nil
}

func validateSnapshotRestoreRoot(root, base, id string) (string, error) {
	resolved, err := validateSnapshotDirectory(root)
	if err != nil {
		return "", err
	}
	if base == "" || !filepath.IsAbs(base) {
		return "", errors.New("快照库必须是绝对路径")
	}
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("无法解析快照库: %w", err)
	}
	baseResolved = filepath.Clean(baseResolved)
	if desktopPathWithin(baseResolved, resolved) || desktopPathWithin(resolved, baseResolved) {
		return "", errors.New("恢复目标不能与快照库重叠")
	}
	if !validSnapshotDescriptionID(id) {
		return "", errors.New("快照 ID 无效")
	}
	return resolved, nil
}

func desktopPathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validSnapshotDescriptionID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.HasPrefix(id, ".") || strings.ContainsAny(id, "/\\\x00") {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func snapshotDescriptionPath(base, id string) (string, error) {
	if !validSnapshotDescriptionID(id) {
		return "", errors.New("快照 ID 无效")
	}
	if base == "" || !filepath.IsAbs(base) {
		return "", errors.New("快照库必须是绝对路径")
	}
	return filepath.Join(base, ".description-"+id+".txt"), nil
}

func readSnapshotDescription(base, id string) (string, error) {
	path, err := snapshotDescriptionPath(base, id)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxSnapshotDescription {
		return "", errors.New("快照说明文件无效")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", errors.New("快照说明文件在打开时已变化")
	}
	data, err := ioReadAllLimited(f, maxSnapshotDescription)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func ioReadAllLimited(f *os.File, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("快照说明文件过大")
	}
	return data, nil
}

func writeSnapshotDescription(base, id, description string) error {
	path, err := snapshotDescriptionPath(base, id)
	if err != nil {
		return err
	}
	if len([]rune(description)) > 500 || strings.ContainsAny(description, "\x00\r\n") {
		return errors.New("快照说明包含无效字符或过长")
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("快照库不是真实目录")
	}
	tmp, err := os.CreateTemp(base, ".description-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.WriteString(strings.TrimSpace(description)); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	keep = true
	return nil
}

func (a *SnapshotManagerApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.Text(c, r.X+10, r.Y+10, d.tr("快照库:"), ui.ColText)
	a.baseR = ui.Rect{r.X + 62, r.Y + 5, r.W - 62 - 94, 25}
	c.FillRect(a.baseR, ui.ColField)
	c.Sunken(a.baseR)
	f.TextClip(c, a.baseR.X+5, a.baseR.Y+6, a.baseR.W-10, a.baseDir, ui.ColText)
	a.refreshR = ui.Rect{r.Right() - 86, r.Y + 5, 78, 25}
	ui.Button(c, a.refreshR, d.tr("刷新"), a.press == "refresh", !a.busy)
	c.FillRect(ui.Rect{r.X + 8, r.Y + 39, r.W - 16, 28}, ui.ColYellow)
	f.TextBold(c, r.X+15, r.Y+47, d.tr("恢复会覆盖快照内的当前工作；必须选择快照、确认目标目录，并经过两次醒目确认。"), ui.ColRed)

	list := ui.Rect{r.X + 8, r.Y + 82, r.W - 16, r.Bottom() - r.Y - 82 - 104}
	if list.H < 80 {
		list.H = 80
	}
	a.listR = list
	c.FillRect(list, ui.ColField)
	c.Sunken(list)
	inner := list.Inset(2)
	scrollW := 16
	tableW := inner.W - scrollW - 2
	headerH, rowH := 20, 24
	c.FillRect(ui.Rect{inner.X, inner.Y, tableW, headerH}, ui.ColFace)
	idW, timeW, sizeW := 208, 132, 82
	f.Text(c, inner.X+4, inner.Y+4, "ID", ui.ColText)
	f.Text(c, inner.X+idW, inner.Y+4, d.tr("时间"), ui.ColText)
	f.Text(c, inner.X+idW+timeW, inner.Y+4, d.tr("大小"), ui.ColText)
	f.Text(c, inner.X+idW+timeW+sizeW, inner.Y+4, d.tr("说明"), ui.ColText)
	rows := maxInt(1, (inner.H-headerH-2)/rowH)
	a.scroll.Visible = rows
	a.scroll.Total = len(a.items)
	a.scroll.Clamp()
	a.rowRects = a.rowRects[:0]
	y := inner.Y + headerH + 1
	for i := a.scroll.Pos; i < len(a.items) && y+rowH <= inner.Bottom(); i++ {
		item := a.items[i]
		rr := ui.Rect{inner.X + 1, y, tableW - 2, rowH - 1}
		a.rowRects = append(a.rowRects, rr)
		col := ui.ColText
		if a.chosen && i == a.sel {
			c.FillRect(rr, ui.ColNavy)
			col = ui.ColWhite
		}
		ui.DrawIcon16(c, ui.IcoDrive, rr.X+3, rr.Y+3)
		f.TextClip(c, rr.X+23, rr.Y+5, idW-27, item.snapshot.ID, col)
		f.TextClip(c, rr.X+idW, rr.Y+5, timeW, item.snapshot.CreatedAt.Local().Format("2006-01-02 15:04:05"), col)
		f.TextClip(c, rr.X+idW+timeW, rr.Y+5, sizeW, diskSizeTextForUI(d, uint64SnapshotBytes(item.snapshot.Bytes)), col)
		f.TextClip(c, rr.X+idW+timeW+sizeW, rr.Y+5, maxInt(40, tableW-idW-timeW-sizeW-8), snapshotDescriptionText(d, item.description), col)
		y += rowH
	}
	if len(a.items) == 0 {
		f.Text(c, inner.X+6, inner.Y+headerH+7, d.tr("没有快照。请先明确选择当前工作目录并创建快照。"), ui.ColShadow)
	}
	a.scrollR = ui.Rect{inner.Right() - scrollW, inner.Y + headerH + 1, scrollW, inner.H - headerH - 2}
	a.scroll.Draw(c, a.scrollR)

	detailsY := list.Bottom() + 8
	if item := a.selected(); item != nil {
		f.TextClip(c, r.X+10, detailsY, r.W-20, d.trf("工作目录: %s", item.snapshot.Root), ui.ColText)
		f.TextClip(c, r.X+10, detailsY+17, r.W-20, d.trf("文件: %d  目录: %d  符号链接: %d", item.snapshot.Files, item.snapshot.Directories, item.snapshot.Symlinks), ui.ColShadow)
	} else {
		f.Text(c, r.X+10, detailsY, d.tr("未选择快照。默认不会选中任何快照或当前工作目录。"), ui.ColShadow)
	}
	if a.progress != "" {
		f.TextClip(c, r.X+10, detailsY+34, r.W-20, a.progress, ui.ColShadow)
	} else if a.err != "" {
		f.TextClip(c, r.X+10, detailsY+34, r.W-20, a.err, ui.ColRed)
	} else {
		f.TextClip(c, r.X+10, detailsY+34, r.W-20, d.tr(a.status), ui.ColShadow)
	}

	buttonY := r.Bottom() - 58
	gap := 8
	width := (r.W - 16 - gap*4) / 5
	rect := func(i int) ui.Rect { return ui.Rect{r.X + 8 + i*(width+gap), buttonY, width, 25} }
	a.createR, a.descriptionR, a.restoreR, a.closeR = rect(0), rect(1), rect(2), rect(3)
	ui.Button(c, a.createR, d.tr("创建快照"), a.press == "create", !a.busy)
	ui.Button(c, a.descriptionR, d.tr("编辑说明"), a.press == "description", !a.busy && a.selected() != nil)
	ui.Button(c, a.restoreR, d.tr("恢复快照"), a.press == "restore", !a.busy && a.selected() != nil)
	ui.Button(c, a.closeR, d.tr("关闭"), a.press == "close", !a.busy)
}

func (a *SnapshotManagerApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if changed, consumed := a.scroll.Mouse(ev, a.scrollR); changed || consumed {
		if ev.Kind == MouseUp {
			a.press = ""
		}
		d.Invalidate()
		return
	}
	if ev.Kind == MouseWheel {
		if a.listR.Contains(ev.X, ev.Y) {
			a.scroll.ScrollBy(-ev.Wheel)
		}
		d.Invalidate()
		return
	}
	if ev.Button != 1 || a.busy {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.refreshR.Contains(ev.X, ev.Y):
			a.press = "refresh"
		case a.baseR.Contains(ev.X, ev.Y):
			a.press = "base"
		case a.createR.Contains(ev.X, ev.Y):
			a.press = "create"
		case a.descriptionR.Contains(ev.X, ev.Y) && a.selected() != nil:
			a.press = "description"
		case a.restoreR.Contains(ev.X, ev.Y) && a.selected() != nil:
			a.press = "restore"
		case a.closeR.Contains(ev.X, ev.Y):
			a.press = "close"
		default:
			for i, rr := range a.rowRects {
				if rr.Contains(ev.X, ev.Y) {
					a.sel, a.chosen = a.scroll.Pos+i, true
					a.status = d.trf("已明确选择快照 %s", a.items[a.sel].snapshot.ID)
					a.press = "row"
					break
				}
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "refresh":
			if a.refreshR.Contains(ev.X, ev.Y) {
				a.refresh()
			}
		case "base":
			if a.baseR.Contains(ev.X, ev.Y) {
				a.setBaseDir()
			}
		case "create":
			if a.createR.Contains(ev.X, ev.Y) {
				a.createSelected()
			}
		case "description":
			if a.descriptionR.Contains(ev.X, ev.Y) {
				a.editDescription()
			}
		case "restore":
			if a.restoreR.Contains(ev.X, ev.Y) {
				a.restoreSelected()
			}
		case "close":
			if a.closeR.Contains(ev.X, ev.Y) {
				d.CloseWindow(w)
			}
		}
	}
	d.Invalidate()
}

func (a *SnapshotManagerApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F5":
		if !a.busy {
			a.refresh()
		}
		return true
	case "Esc":
		if !a.busy {
			d.CloseWindow(w)
		}
		return true
	}
	return false
}
