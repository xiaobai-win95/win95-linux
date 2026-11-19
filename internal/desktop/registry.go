package desktop

import (
	"strings"
	"time"

	"win95linux/internal/ui"
)

// RegistryApp edits /etc/win95.ini through a tree of sections and a value
// list. Saving validates the document, writes it and applies it to the live
// desktop immediately.
type RegistryApp struct {
	Base
	d   *Desktop
	win *Window

	doc     *iniDoc
	tree    *regTreeNode
	visible []*regTreeNode
	depths  []int
	selNode int

	values []regValueRow
	selVal int
	scroll VScroll

	dirty     bool
	status    string
	press     string
	lastClick time.Time
	lastVal   int

	treeR, valuesR, statusR                        ui.Rect
	btnSave, btnReload, btnEdit, btnNew, btnDelete ui.Rect
}

type regTreeNode struct {
	label    string
	section  string
	children []*regTreeNode
	expanded bool
}

type regValueRow struct {
	key   string
	value string
}

func NewRegistryApp(d *Desktop) *RegistryApp {
	a := &RegistryApp{d: d, selVal: -1, lastVal: -1}
	a.reload(false)
	return a
}

func (a *RegistryApp) Title() string           { return "注册表编辑器" }
func (a *RegistryApp) Icon() ui.IconKind       { return ui.IcoSetup }
func (a *RegistryApp) DefaultSize() (int, int) { return 740, 520 }
func (a *RegistryApp) MinSize() (int, int)     { return 580, 380 }
func (a *RegistryApp) Resizable() bool         { return true }

func (a *RegistryApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "注册表(R)", Items: []MenuItem{
			{Label: "保存(S)", Accel: "Ctrl+S", Action: func() { a.save(d) }},
			{Label: "重新载入(L)", Accel: "F5", Action: func() { a.reload(true) }},
			{Separator: true},
			{Label: "关闭(C)", Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "编辑(E)", Items: []MenuItem{
			{Label: "修改值(M)", Accel: "Enter", Enabled: a.selVal >= 0, Action: func() { a.editValue(d) }},
			{Label: "新建值(N)", Enabled: a.section() != "", Action: func() { a.newValue(d) }},
			{Label: "删除值(D)", Accel: "Del", Enabled: a.selVal >= 0, Action: func() { a.deleteValue(d) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于注册表编辑器(A)", Action: func() {
				d.MessageBox("关于注册表编辑器", "Windows 95 Linux 注册表编辑器\n版本 1.0\n\n配置文件：/etc/win95.ini\n\n保存后桌面背景、主题、语言与字体立即更新，无需重启。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *RegistryApp) reload(showStatus bool) {
	doc, err := readWin95INI()
	if err != nil {
		a.status = "读取 /etc/win95.ini 失败：" + err.Error()
		if a.doc == nil {
			a.doc = defaultWin95INI()
		}
	} else {
		a.doc = doc
		if showStatus {
			a.status = "已从 /etc/win95.ini 重新载入。"
		}
	}
	keep := a.section()
	a.buildTree()
	a.flatten()
	a.selectSection(keep)
	a.dirty = false
}

func (a *RegistryApp) buildTree() {
	root := &regTreeNode{label: "我的电脑", expanded: true}
	hklm := &regTreeNode{label: "HKEY_LOCAL_MACHINE", expanded: true}
	software := &regTreeNode{label: "SOFTWARE", expanded: true}
	win95 := &regTreeNode{label: "Win95Linux", expanded: true}
	for _, sec := range a.doc.Sections {
		if strings.EqualFold(sec.Name, "General") {
			continue
		}
		win95.children = append(win95.children, &regTreeNode{label: sec.Name, section: sec.Name})
	}
	software.children = []*regTreeNode{win95}
	hklm.children = []*regTreeNode{software}
	root.children = []*regTreeNode{hklm}
	a.tree = root
}

func (a *RegistryApp) flatten() {
	a.visible = a.visible[:0]
	a.depths = a.depths[:0]
	var walk func(n *regTreeNode, depth int)
	walk = func(n *regTreeNode, depth int) {
		a.visible = append(a.visible, n)
		a.depths = append(a.depths, depth)
		if !n.expanded {
			return
		}
		for _, ch := range n.children {
			walk(ch, depth+1)
		}
	}
	walk(a.tree, 0)
}

func (a *RegistryApp) section() string {
	if a.selNode >= 0 && a.selNode < len(a.visible) {
		return a.visible[a.selNode].section
	}
	return ""
}

func (a *RegistryApp) selectSection(name string) {
	a.selNode = 0
	for i := 1; i < len(a.visible); i++ {
		if name != "" && strings.EqualFold(a.visible[i].section, name) {
			a.selNode = i
			break
		}
	}
	if name == "" {
		for i := 1; i < len(a.visible); i++ {
			if a.visible[i].section != "" {
				a.selNode = i
				break
			}
		}
	}
	a.loadValues()
}

func (a *RegistryApp) loadValues() {
	a.values = a.values[:0]
	a.selVal = -1
	name := a.section()
	if name == "" {
		a.scroll.Pos = 0
		return
	}
	for _, sec := range a.doc.Sections {
		if strings.EqualFold(sec.Name, name) {
			for _, kv := range sec.Keys {
				a.values = append(a.values, regValueRow{key: kv.Key, value: kv.Value})
			}
			break
		}
	}
	if len(a.values) > 0 {
		a.selVal = 0
	}
	a.scroll.Pos = 0
}

func (a *RegistryApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()

	// Toolbar
	ty := r.Y + 4
	a.btnSave = ui.Rect{r.X + 4, ty, 64, 24}
	a.btnReload = ui.Rect{a.btnSave.Right() + 4, ty, 76, 24}
	a.btnEdit = ui.Rect{a.btnReload.Right() + 16, ty, 64, 24}
	a.btnNew = ui.Rect{a.btnEdit.Right() + 4, ty, 76, 24}
	a.btnDelete = ui.Rect{a.btnNew.Right() + 4, ty, 76, 24}
	ui.Button(c, a.btnSave, "保存", a.press == "save", true)
	ui.Button(c, a.btnReload, "重新载入", a.press == "reload", true)
	ui.Button(c, a.btnEdit, "修改", a.press == "edit", a.selVal >= 0)
	ui.Button(c, a.btnNew, "新建值", a.press == "new", a.section() != "")
	ui.Button(c, a.btnDelete, "删除值", a.press == "del", a.selVal >= 0)
	hint := "双击值可修改"
	if a.dirty {
		hint = "有未保存的更改"
	}
	f.TextRight(c, r.Right()-8, ty+4, hint, ui.ColShadow)
	c.HLine(r.X+4, r.Y+31, r.W-8, ui.ColShadow)
	c.HLine(r.X+4, r.Y+32, r.W-8, ui.ColWhite)

	statusH := 22
	bodyY := r.Y + 36
	bodyH := r.Bottom() - statusH - bodyY
	if bodyH < 40 {
		bodyH = 40
	}
	treeW := 210
	a.treeR = ui.Rect{r.X + 4, bodyY, treeW, bodyH}
	a.valuesR = ui.Rect{a.treeR.Right() + 5, bodyY, r.W - treeW - 13, bodyH}
	c.FillRect(a.treeR, ui.ColWhite)
	c.Sunken(a.treeR)
	c.FillRect(a.valuesR, ui.ColWhite)
	c.Sunken(a.valuesR)

	a.paintTree(c, f)
	a.paintValues(c, f)

	a.statusR = ui.Rect{r.X, r.Bottom() - statusH, r.W, statusH}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	path := `HKEY_LOCAL_MACHINE\SOFTWARE\Win95Linux`
	if s := a.section(); s != "" {
		path += `\` + s
	}
	if a.dirty {
		path += " *"
	}
	info := path
	if a.status != "" {
		info += "　·　" + a.status
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+4, a.statusR.W-12, info, ui.ColText)
}

func (a *RegistryApp) paintTree(c *ui.Canvas, f *ui.Font) {
	inner := a.treeR.Inset(2)
	rowH := 20
	y := inner.Y + 2
	for i, n := range a.visible {
		if y+rowH > inner.Bottom() {
			break
		}
		rr := ui.Rect{inner.X, y, inner.W, rowH}
		if i == a.selNode {
			c.FillRect(rr, ui.ColNavy)
		}
		x := inner.X + 4 + a.depths[i]*16
		if len(n.children) > 0 {
			tb := ui.Rect{x, y + 5, 9, 9}
			c.FillRect(tb, ui.ColWhite)
			c.Frame(tb, ui.ColShadow)
			c.HLine(tb.X+2, tb.Y+4, tb.W-4, ui.ColText)
			if !n.expanded {
				c.VLine(tb.X+tb.W/2, tb.Y+2, tb.H-4, ui.ColText)
			}
			x += 13
		}
		kind := ui.IcoFolder
		if n.section != "" {
			kind = ui.IcoText
		} else if n.expanded && len(n.children) > 0 {
			kind = ui.IcoFolderOpen
		}
		ui.DrawIcon16(c, kind, x, y+2)
		col := ui.ColText
		if i == a.selNode {
			col = ui.ColWhite
		}
		f.TextClip(c, x+20, y+2, inner.Right()-x-24, n.label, col)
		y += rowH
	}
}

func (a *RegistryApp) paintValues(c *ui.Canvas, f *ui.Font) {
	inner := a.valuesR.Inset(2)
	headH := 20
	scrollW := 16
	colW := inner.W - scrollW - 2 - 190
	if colW < 120 {
		colW = 120
	}
	c.FillRect(ui.Rect{inner.X, inner.Y, inner.W - scrollW, headH}, ui.ColFace)
	f.Text(c, inner.X+6, inner.Y+3, "名称", ui.ColText)
	f.Text(c, inner.X+6+colW, inner.Y+3, "数据", ui.ColText)
	c.HLine(inner.X, inner.Y+headH-1, inner.W-scrollW, ui.ColShadow)

	if a.section() == "" {
		f.Text(c, inner.X+10, inner.Y+headH+8, "请展开左侧目录并选择一个配置节。", ui.ColShadow)
		return
	}
	rows := (inner.H - headH - 2) / 21
	if rows < 1 {
		rows = 1
	}
	a.scroll.Total = len(a.values)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	y := inner.Y + headH + 1
	for i := a.scroll.Pos; i < len(a.values) && y+20 <= inner.Bottom(); i++ {
		v := a.values[i]
		rr := ui.Rect{inner.X + 1, y, inner.W - scrollW - 3, 20}
		if i == a.selVal {
			c.FillRect(rr, ui.ColNavy)
		}
		col := ui.ColText
		if i == a.selVal {
			col = ui.ColWhite
		}
		f.TextClip(c, rr.X+5, rr.Y+2, colW-8, v.key, col)
		shown := v.value
		if shown == "" {
			shown = "(空)"
		}
		f.TextClip(c, rr.X+5+colW, rr.Y+2, inner.W-scrollW-colW-10, shown, col)
		y += 21
	}
	if len(a.values) == 0 {
		f.Text(c, inner.X+10, inner.Y+headH+8, "此配置节没有值。", ui.ColShadow)
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW - 2, inner.Y + headH + 1, scrollW, inner.H - headH - 2})
}

func (a *RegistryApp) valueRowAt(x, y int) int {
	inner := a.valuesR.Inset(2)
	headH := 20
	if y < inner.Y+headH+1 || y >= inner.Bottom() {
		return -1
	}
	row := (y - inner.Y - headH - 1) / 21
	idx := a.scroll.Pos + row
	if idx >= 0 && idx < len(a.values) {
		return idx
	}
	return -1
}

func (a *RegistryApp) treeRowAt(x, y int) int {
	inner := a.treeR.Inset(2)
	if y < inner.Y+2 {
		return -1
	}
	idx := (y - inner.Y - 2) / 20
	if idx >= 0 && idx < len(a.visible) {
		return idx
	}
	return -1
}

func (a *RegistryApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if changed, _ := a.scroll.Mouse(ev, ui.Rect{a.valuesR.Inset(2).Right() - 18, a.valuesR.Inset(2).Y + 21, 16, a.valuesR.Inset(2).H - 23}); changed {
		d.Invalidate()
	}
	if ev.Kind == MouseWheel {
		return
	}
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.btnSave.Contains(ev.X, ev.Y):
			a.press = "save"
		case a.btnReload.Contains(ev.X, ev.Y):
			a.press = "reload"
		case a.btnEdit.Contains(ev.X, ev.Y):
			a.press = "edit"
		case a.btnNew.Contains(ev.X, ev.Y):
			a.press = "new"
		case a.btnDelete.Contains(ev.X, ev.Y):
			a.press = "del"
		case a.treeR.Contains(ev.X, ev.Y):
			idx := a.treeRowAt(ev.X, ev.Y)
			if idx >= 0 {
				n := a.visible[idx]
				inner := a.treeR.Inset(2)
				rowY := inner.Y + 2 + idx*20
				x := inner.X + 4 + a.depths[idx]*16
				toggle := ui.Rect{x, rowY + 5, 9, 9}
				if len(n.children) > 0 && toggle.Contains(ev.X, ev.Y) {
					n.expanded = !n.expanded
					keep := a.section()
					a.flatten()
					a.selectSection(keep)
					d.Invalidate()
					return
				}
				if idx != a.selNode {
					a.selNode = idx
					a.loadValues()
					d.Invalidate()
				}
			}
		case a.valuesR.Contains(ev.X, ev.Y):
			idx := a.valueRowAt(ev.X, ev.Y)
			if idx >= 0 {
				a.selVal = idx
				if time.Since(a.lastClick) < 450*time.Millisecond && a.lastVal == idx {
					a.lastVal = -1
					a.editValue(d)
					return
				}
				a.lastClick = time.Now()
				a.lastVal = idx
				d.Invalidate()
			}
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "save":
			if a.btnSave.Contains(ev.X, ev.Y) {
				a.save(d)
			}
		case "reload":
			if a.btnReload.Contains(ev.X, ev.Y) {
				a.reload(true)
				d.Invalidate()
			}
		case "edit":
			if a.btnEdit.Contains(ev.X, ev.Y) {
				a.editValue(d)
			}
		case "new":
			if a.btnNew.Contains(ev.X, ev.Y) {
				a.newValue(d)
			}
		case "del":
			if a.btnDelete.Contains(ev.X, ev.Y) {
				a.deleteValue(d)
			}
		}
	}
}

func validRegValue(s string) string {
	if strings.ContainsAny(s, "\r\n") {
		return "值不能包含换行。"
	}
	if len([]rune(s)) > 500 {
		return "值太长了。"
	}
	return ""
}

func (a *RegistryApp) editValue(d *Desktop) {
	if a.selVal < 0 || a.selVal >= len(a.values) {
		return
	}
	row := a.values[a.selVal]
	sec := a.section()
	d.InputBoxChecked("修改值", "值名称："+row.key+"    当前数据："+row.value, row.value, validRegValue,
		func(text string, ok bool) {
			if !ok {
				return
			}
			a.doc.Set(sec, row.key, text)
			a.dirty = true
			a.status = "已修改，按“保存”后立即生效。"
			a.loadValues()
			for i, v := range a.values {
				if strings.EqualFold(v.key, row.key) {
					a.selVal = i
					break
				}
			}
			d.Invalidate()
		})
}

func (a *RegistryApp) newValue(d *Desktop) {
	sec := a.section()
	if sec == "" {
		return
	}
	d.InputBoxChecked("新建值", "配置节："+sec+"    输入新值的名称：", "", func(name string) string {
		if name == "" {
			return "名称不能为空。"
		}
		if strings.ContainsAny(name, "=[]\r\n") {
			return "名称中不能包含 = [ ] 或换行。"
		}
		if a.doc.Get(sec, name) != "" {
			return "该名称已存在。"
		}
		for _, v := range a.values {
			if strings.EqualFold(v.key, name) {
				return "该名称已存在。"
			}
		}
		return ""
	}, func(name string, ok bool) {
		if !ok {
			return
		}
		d.InputBoxChecked("新建值", "值名称："+name+"    输入数据：", "", validRegValue,
			func(text string, ok bool) {
				if !ok {
					return
				}
				a.doc.Set(sec, name, text)
				a.dirty = true
				a.status = "已新建值，按“保存”后立即生效。"
				a.loadValues()
				for i, v := range a.values {
					if strings.EqualFold(v.key, name) {
						a.selVal = i
						break
					}
				}
				d.Invalidate()
			})
	})
}

func (a *RegistryApp) deleteValue(d *Desktop) {
	if a.selVal < 0 || a.selVal >= len(a.values) {
		return
	}
	row := a.values[a.selVal]
	sec := a.section()
	d.MessageBox("删除值", "确实要删除值“"+row.key+"”吗？\n\n按“保存”后生效。", IconQuestion,
		[]string{"是", "否"}, func(i int) {
			if i != 0 {
				return
			}
			a.doc.Remove(sec, row.key)
			a.dirty = true
			a.status = "已删除值，按“保存”后立即生效。"
			a.loadValues()
			d.Invalidate()
		})
}

// save validates, persists and hot-applies the document. It reports whether
// the file was written and applied.
func (a *RegistryApp) save(d *Desktop) bool {
	if err := validateWin95Doc(a.doc); err != nil {
		d.MessageBox("注册表编辑器", "无法保存：\n\n"+err.Error(), IconError, []string{"确定"}, nil)
		return false
	}
	if err := writeWin95INI(a.doc); err != nil {
		d.MessageBox("注册表编辑器", "无法写入 /etc/win95.ini：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return false
	}
	oldLang := d.lang
	d.applyWin95Doc(a.doc)
	if d.lang != oldLang {
		d.applyLanguage(d.lang)
	} else {
		d.savePrefs()
	}
	a.dirty = false
	a.status = "已保存并立即生效 · " + time.Now().Format("15:04:05")
	d.Invalidate()
	return true
}

func (a *RegistryApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch {
	case ev.Name == "F5":
		a.reload(true)
		d.Invalidate()
		return true
	case ev.Name == "Enter":
		a.editValue(d)
		return true
	case ev.Name == "Delete":
		a.deleteValue(d)
		return true
	case ev.Name == "Down":
		if a.selVal >= 0 && a.selVal < len(a.values)-1 {
			a.selVal++
			d.Invalidate()
		}
		return true
	case ev.Name == "Up":
		if a.selVal > 0 {
			a.selVal--
			d.Invalidate()
		}
		return true
	case ev.Name == "PgDn":
		if n := len(a.values); n > 0 {
			a.selVal += 10
			if a.selVal >= n {
				a.selVal = n - 1
			}
			d.Invalidate()
		}
		return true
	case ev.Name == "PgUp":
		if len(a.values) > 0 {
			a.selVal -= 10
			if a.selVal < 0 {
				a.selVal = 0
			}
			d.Invalidate()
		}
		return true
	}
	if ev.Ctrl && ev.Name == "S" {
		a.save(d)
		return true
	}
	return false
}

func (a *RegistryApp) RequestClose(d *Desktop, w *Window) bool {
	if !a.dirty {
		return true
	}
	d.MessageBox("注册表编辑器", "注册表已经修改。\n\n是否保存更改？", IconQuestion,
		[]string{"保存", "放弃", "取消"}, func(i int) {
			switch i {
			case 0:
				if a.save(d) {
					d.CloseWindowForced(w)
				}
			case 1:
				d.CloseWindowForced(w)
			}
		})
	return false
}

func (d *Desktop) openRegistryEditor() {
	for _, w := range d.windows {
		if _, ok := w.App.(*RegistryApp); ok {
			d.bringToFront(w)
			return
		}
	}
	d.Open(NewRegistryApp(d))
}
