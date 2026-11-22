package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"win95linux/internal/ui"
)

var macroDir = "/root/宏"

type macroEvent struct {
	T      int    `json:"t"`
	Type   string `json:"type"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Button int    `json:"button,omitempty"`
	Wheel  int    `json:"wheel,omitempty"`
	Rune   rune   `json:"rune,omitempty"`
	Name   string `json:"name,omitempty"`
	Shift  bool   `json:"shift,omitempty"`
	Ctrl   bool   `json:"ctrl,omitempty"`
	Alt    bool   `json:"alt,omitempty"`
}

type macroScript struct {
	Name    string       `json:"name"`
	Created string       `json:"created"`
	Events  []macroEvent `json:"events"`
}

type MacroApp struct {
	Base
	d   *Desktop
	win *Window

	script  macroScript
	scripts []string
	sel     int

	recording bool
	recStart  time.Time
	lastMove  time.Time

	playing   bool
	playStart time.Time
	playIdx   int
	playLoop  int
	loops     int
	interval  int

	status string
	press  string
}

func NewMacroApp(d *Desktop) *MacroApp {
	a := &MacroApp{d: d, loops: 1, interval: 400, sel: -1,
		status: "点击“录制”，然后在桌面上操作；点击“停止”结束录制。"}
	a.refreshScripts()
	return a
}

func (a *MacroApp) Title() string           { return "按键精灵" }
func (a *MacroApp) Icon() ui.IconKind       { return ui.IcoMacro }
func (a *MacroApp) DefaultSize() (int, int) { return 720, 480 }
func (a *MacroApp) MinSize() (int, int)     { return 620, 400 }
func (a *MacroApp) Resizable() bool         { return true }

func (d *Desktop) openMacro() {
	for _, w := range d.windows {
		if _, ok := w.App.(*MacroApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewMacroApp(d)
	a.win = d.Open(a)
	d.macro = a
}

func (a *MacroApp) Close() {
	a.recording = false
	a.playing = false
	if a.d != nil && a.d.macro == a {
		a.d.macro = nil
	}
}

type macroLayout struct {
	status  ui.Rect
	list    ui.Rect
	buttons map[string]ui.Rect

	panelX, panelY                        int
	loopLbl, loopMinus, loopVal, loopPlus ui.Rect
	intLbl, intMinus, intVal, intPlus     ui.Rect
}

func macroLayoutFor(r ui.Rect) macroLayout {
	l := macroLayout{buttons: map[string]ui.Rect{}}
	y := r.Y + 6
	l.status = ui.Rect{r.X + 8, y, r.W - 16, 18}
	y += 24
	x := r.X + 8
	for _, b := range []struct {
		id string
		w  int
	}{{"rec", 78}, {"stop", 78}, {"play", 78}, {"save", 78}, {"load", 78}, {"refresh", 64}} {
		l.buttons[b.id] = ui.Rect{x, y, b.w, 26}
		x += b.w + 4
	}
	y += 36
	listW := 230
	if r.W < 640 {
		listW = 200
	}
	l.list = ui.Rect{r.X + 8, y, listW, r.Bottom() - y - 40}
	if l.list.H < 100 {
		l.list.H = 100
	}
	l.panelX = l.list.Right() + 14
	l.panelY = y

	ly := y + 118
	l.loopLbl = ui.Rect{l.panelX, ly, 86, 22}
	l.loopMinus = ui.Rect{l.panelX + 88, ly, 26, 22}
	l.loopVal = ui.Rect{l.panelX + 118, ly, 46, 22}
	l.loopPlus = ui.Rect{l.panelX + 168, ly, 26, 22}
	iy := ly + 30
	l.intLbl = ui.Rect{l.panelX, iy, 86, 22}
	l.intMinus = ui.Rect{l.panelX + 88, iy, 26, 22}
	l.intVal = ui.Rect{l.panelX + 118, iy, 46, 22}
	l.intPlus = ui.Rect{l.panelX + 168, iy, 26, 22}
	return l
}

func (a *MacroApp) modeText() string {
	switch {
	case a.recording:
		return "录制中"
	case a.playing:
		return "回放中"
	default:
		return "空闲"
	}
}

func (a *MacroApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	l := macroLayoutFor(r)

	f.TextClip(c, l.status.X, l.status.Y, l.status.W, a.status, ui.ColText)

	btn := func(id, label string, pressed, enabled bool) {
		ui.Button(c, l.buttons[id], label, pressed, enabled)
	}
	btn("rec", "录制", a.recording, !a.recording)
	btn("stop", "停止", false, a.recording || a.playing)
	btn("play", "回放", a.playing, len(a.script.Events) > 0 && !a.playing)
	btn("save", "保存", false, len(a.script.Events) > 0)
	btn("load", "加载", false, len(a.scripts) > 0)
	btn("refresh", "刷新", false, true)

	c.FillRect(l.list, 0xFFFFFF)
	c.Sunken(l.list)
	f.Text(c, l.list.X+4, l.list.Y+3, "脚本（再次点击可加载）", ui.ColText)
	rowY := l.list.Y + 22
	for i, p := range a.scripts {
		if rowY+18 > l.list.Bottom()-2 {
			break
		}
		row := ui.Rect{l.list.X + 2, rowY, l.list.W - 4, 18}
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if i == a.sel {
			c.FillRect(row, ui.ColNavy)
			f.TextClip(c, row.X+4, row.Y+2, row.W-8, name, ui.ColWhite)
		} else {
			f.TextClip(c, row.X+4, row.Y+2, row.W-8, name, ui.ColText)
		}
		rowY += 19
	}
	if len(a.scripts) == 0 {
		f.Text(c, l.list.X+4, l.list.Y+26, "（还没有保存的脚本）", ui.ColTextDis)
	}

	px := l.panelX
	f.TextClip(c, px, l.panelY, r.Right()-8-px, "录制桌面鼠标与键盘，保存为 JSON 脚本。", ui.ColText)
	f.TextClip(c, px, l.panelY+18, r.Right()-8-px, "回放会按录制时的时间间隔注入事件。", ui.ColText)
	f.TextClip(c, px, l.panelY+36, r.Right()-8-px, "录制时点击本窗口不会被记录。", ui.ColText)
	f.TextClip(c, px, l.panelY+54, r.Right()-8-px, "回放中按 Esc 可立即停止。", ui.ColText)
	f.TextClip(c, px, l.panelY+82, r.Right()-8-px,
		fmt.Sprintf("事件：%d    状态：%s", len(a.script.Events), a.modeText()), ui.ColText)
	name := a.script.Name
	if name == "" {
		name = "（未命名）"
	}
	f.TextClip(c, px, l.panelY+100, r.Right()-8-px, "脚本："+name, ui.ColText)

	f.Text(c, l.loopLbl.X, l.loopLbl.Y+4, "循环次数", ui.ColText)
	ui.Button(c, l.loopMinus, "-", false, a.loops > 1)
	c.FillRect(l.loopVal, 0xFFFFFF)
	c.Sunken(l.loopVal)
	f.TextRight(c, l.loopVal.Right()-4, l.loopVal.Y+4, fmt.Sprintf("%d", a.loops), ui.ColText)
	ui.Button(c, l.loopPlus, "+", false, a.loops < 999)

	f.Text(c, l.intLbl.X, l.intLbl.Y+4, "间隔(毫秒)", ui.ColText)
	ui.Button(c, l.intMinus, "-", false, a.interval > 0)
	c.FillRect(l.intVal, 0xFFFFFF)
	c.Sunken(l.intVal)
	f.TextRight(c, l.intVal.Right()-4, l.intVal.Y+4, fmt.Sprintf("%d", a.interval), ui.ColText)
	ui.Button(c, l.intPlus, "+", false, a.interval < 60000)

	if a.playing {
		f.TextClip(c, px, l.intPlus.Bottom()+12, r.Right()-8-px,
			fmt.Sprintf("进度：%d/%d    第 %d/%d 轮", a.playIdx, len(a.script.Events), a.playLoop+1, a.loops), ui.ColText)
	}

	f.TextClip(c, r.X+8, r.Bottom()-20, r.W-16, "脚本目录："+macroDir, ui.ColTextDis)
}

func macroRowAt(l macroLayout, i int) ui.Rect {
	return ui.Rect{l.list.X + 2, l.list.Y + 22 + i*19, l.list.W - 4, 18}
}

func (a *MacroApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	l := macroLayoutFor(client)
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		for id, r := range l.buttons {
			if r.Contains(ev.X, ev.Y) {
				a.press = id
				d.Invalidate()
				return
			}
		}
		switch {
		case l.loopMinus.Contains(ev.X, ev.Y):
			if a.loops > 1 {
				a.loops--
				d.Invalidate()
			}
		case l.loopPlus.Contains(ev.X, ev.Y):
			if a.loops < 999 {
				a.loops++
				d.Invalidate()
			}
		case l.intMinus.Contains(ev.X, ev.Y):
			if a.interval >= 100 {
				a.interval -= 100
			} else {
				a.interval = 0
			}
			d.Invalidate()
		case l.intPlus.Contains(ev.X, ev.Y):
			if a.interval < 60000 {
				a.interval += 100
			}
			d.Invalidate()
		default:
			for i := range a.scripts {
				if !macroRowAt(l, i).Contains(ev.X, ev.Y) {
					continue
				}
				if i == a.sel {
					a.loadPath(d, a.scripts[i])
				} else {
					a.sel = i
					d.Invalidate()
				}
				return
			}
		}
	case MouseUp:
		if a.press == "" {
			return
		}
		id := a.press
		if r, ok := l.buttons[id]; ok && r.Contains(ev.X, ev.Y) {
			a.press = ""
			a.action(d, id)
		}
		d.Invalidate()
	}
}

func (a *MacroApp) action(d *Desktop, id string) {
	switch id {
	case "rec":
		a.startRecording(d)
	case "stop":
		a.stopAll(d)
	case "play":
		a.startPlay(d)
	case "save":
		a.saveScript(d)
	case "load":
		if a.sel >= 0 && a.sel < len(a.scripts) {
			a.loadPath(d, a.scripts[a.sel])
		}
	case "refresh":
		a.refreshScripts()
		a.status = fmt.Sprintf("已刷新，共 %d 个脚本", len(a.scripts))
	}
}

func (a *MacroApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" && (a.recording || a.playing) {
		a.stopAll(d)
		return true
	}
	if ev.Ctrl && (ev.Rune == 'r' || ev.Rune == 'R') {
		a.startRecording(d)
		return true
	}
	if ev.Name == "F5" {
		a.startPlay(d)
		return true
	}
	return false
}

func (a *MacroApp) startRecording(d *Desktop) {
	if a.recording {
		return
	}
	if a.playing {
		a.stopPlay(d)
	}
	if len(a.script.Events) > 0 {
		d.MessageBox("按键精灵", "开始新的录制会清除当前脚本事件。\n继续吗？", IconQuestion, []string{"确定", "取消"}, func(i int) {
			if i == 0 {
				a.beginRecording(d)
			}
		})
		return
	}
	a.beginRecording(d)
}

func (a *MacroApp) beginRecording(d *Desktop) {
	a.script = macroScript{}
	a.sel = -1
	a.recording = true
	a.playing = false
	a.recStart = time.Now()
	a.lastMove = time.Time{}
	a.status = "正在录制…操作完成后点击“停止”。"
	d.Invalidate()
}

func (a *MacroApp) stopAll(d *Desktop) {
	if a.playing {
		a.stopPlay(d)
	}
	if a.recording {
		a.recording = false
		if n := len(a.script.Events); n > 0 {
			if last := a.script.Events[n-1]; last.Type == "key" && last.Name == "Esc" {
				a.script.Events = a.script.Events[:n-1]
			}
		}
		a.status = fmt.Sprintf("录制完成，共 %d 个事件。点击“保存”写入 JSON。", len(a.script.Events))
	}
	d.Invalidate()
}

func (a *MacroApp) startPlay(d *Desktop) {
	if a.playing {
		return
	}
	if a.recording {
		a.stopAll(d)
	}
	if len(a.script.Events) == 0 {
		d.MessageBox("按键精灵", "当前脚本没有事件，请先录制。", IconInfo, []string{"确定"}, nil)
		return
	}
	if a.loops < 1 {
		a.loops = 1
	}
	a.playing = true
	a.playIdx = 0
	a.playLoop = 0
	a.playStart = time.Now()
	a.status = "回放中…按 Esc 停止。"
	d.Invalidate()
}

func (a *MacroApp) stopPlay(d *Desktop) {
	if !a.playing {
		return
	}
	a.playing = false
	a.status = fmt.Sprintf("已停止回放（第 %d 个事件）", a.playIdx)
	d.Invalidate()
}

// step advances playback; the desktop calls it every animation frame so the
// original event timings survive even while the window is minimized.
func (a *MacroApp) step(d *Desktop, now time.Time) {
	if !a.playing {
		return
	}
	elapsed := now.Sub(a.playStart)
	for a.playIdx < len(a.script.Events) {
		ev := a.script.Events[a.playIdx]
		if time.Duration(ev.T)*time.Millisecond > elapsed {
			break
		}
		a.inject(d, ev)
		a.playIdx++
	}
	if a.playIdx >= len(a.script.Events) {
		a.playLoop++
		if a.playLoop >= a.loops {
			a.playing = false
			a.status = fmt.Sprintf("回放完成：%d 轮，共 %d 个事件", a.playLoop, len(a.script.Events))
		} else {
			a.playStart = now.Add(time.Duration(a.interval) * time.Millisecond)
			a.playIdx = 0
			a.status = fmt.Sprintf("回放中：第 %d/%d 轮", a.playLoop+1, a.loops)
		}
	}
	d.Invalidate()
}

func (a *MacroApp) inject(d *Desktop, ev macroEvent) {
	d.Post(func() {
		switch ev.Type {
		case "move":
			d.Mouse(MouseEvent{Kind: MouseMove, X: ev.X, Y: ev.Y, Shift: ev.Shift, Ctrl: ev.Ctrl})
		case "down":
			d.Mouse(MouseEvent{Kind: MouseDown, X: ev.X, Y: ev.Y, Button: ev.Button, Shift: ev.Shift, Ctrl: ev.Ctrl})
		case "up":
			d.Mouse(MouseEvent{Kind: MouseUp, X: ev.X, Y: ev.Y, Button: ev.Button, Shift: ev.Shift, Ctrl: ev.Ctrl})
		case "wheel":
			d.Mouse(MouseEvent{Kind: MouseWheel, X: ev.X, Y: ev.Y, Wheel: ev.Wheel})
		case "key":
			d.Key(KeyEvent{Rune: ev.Rune, Name: ev.Name, Shift: ev.Shift, Ctrl: ev.Ctrl, Alt: ev.Alt})
		}
	})
}

// macroRecordMouse appends mouse events while recording. Events aimed at the
// recorder window itself are skipped so the UI clicks never become part of the
// script.
func (d *Desktop) macroRecordMouse(ev MouseEvent) {
	m := d.macro
	if m == nil || !m.recording || m.playing {
		return
	}
	if m.win != nil && ev.X >= m.win.X && ev.X < m.win.X+m.win.W &&
		ev.Y >= m.win.Y && ev.Y < m.win.Y+m.win.H {
		return
	}
	typ := ""
	switch ev.Kind {
	case MouseMove:
		typ = "move"
	case MouseDown:
		typ = "down"
	case MouseUp:
		typ = "up"
	case MouseWheel:
		typ = "wheel"
	}
	if typ == "" {
		return
	}
	now := time.Now()
	if ev.Kind == MouseMove {
		if n := len(m.script.Events); n > 0 {
			if last := m.script.Events[n-1]; last.Type == "move" && last.X == ev.X && last.Y == ev.Y {
				return
			}
		}
		if !m.lastMove.IsZero() && now.Sub(m.lastMove) < 33*time.Millisecond {
			return
		}
		m.lastMove = now
	}
	m.script.Events = append(m.script.Events, macroEvent{
		T: int(now.Sub(m.recStart).Milliseconds()), Type: typ,
		X: ev.X, Y: ev.Y, Button: ev.Button, Wheel: ev.Wheel,
		Shift: ev.Shift, Ctrl: ev.Ctrl,
	})
}

func (d *Desktop) macroRecordKey(ev KeyEvent) {
	m := d.macro
	if m == nil || !m.recording || m.playing {
		return
	}
	m.script.Events = append(m.script.Events, macroEvent{
		T: int(time.Since(m.recStart).Milliseconds()), Type: "key",
		Rune: ev.Rune, Name: ev.Name, Shift: ev.Shift, Ctrl: ev.Ctrl, Alt: ev.Alt,
	})
}

func (a *MacroApp) refreshScripts() {
	a.scripts = a.scripts[:0]
	ents, err := os.ReadDir(macroDir)
	if err == nil {
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			a.scripts = append(a.scripts, filepath.Join(macroDir, e.Name()))
		}
		sort.Strings(a.scripts)
	}
	if a.sel >= len(a.scripts) {
		a.sel = len(a.scripts) - 1
	}
	if len(a.scripts) == 0 {
		a.sel = -1
	}
}

func normalizedMacroName(name string) string {
	name = strings.TrimSpace(name)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	name = repl.Replace(name)
	if name == "" {
		name = "宏"
	}
	return name
}

func (a *MacroApp) saveScript(d *Desktop) {
	if len(a.script.Events) == 0 {
		d.MessageBox("按键精灵", "没有可保存的事件，请先录制。", IconInfo, []string{"确定"}, nil)
		return
	}
	if strings.TrimSpace(a.script.Name) == "" {
		a.script.Name = "宏-" + time.Now().Format("20060102-150405")
	}
	if a.script.Created == "" {
		a.script.Created = time.Now().Format("2006-01-02 15:04:05")
	}
	if err := os.MkdirAll(macroDir, 0o755); err != nil {
		d.MessageBox("按键精灵", "无法创建脚本目录：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	path := filepath.Join(macroDir, normalizedMacroName(a.script.Name)+".json")
	data, err := json.MarshalIndent(a.script, "", "  ")
	if err != nil {
		d.MessageBox("按键精灵", "无法编码脚本：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		d.MessageBox("按键精灵", "保存失败：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a.refreshScripts()
	for i, p := range a.scripts {
		if p == path {
			a.sel = i
		}
	}
	a.status = "已保存：" + path
	d.Invalidate()
}

func (a *MacroApp) loadPath(d *Desktop, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		d.MessageBox("按键精灵", "无法读取脚本：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	var s macroScript
	if err := json.Unmarshal(data, &s); err != nil {
		d.MessageBox("按键精灵", "脚本格式错误：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	if s.Name == "" {
		s.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	a.script = s
	a.recording = false
	a.playing = false
	a.status = fmt.Sprintf("已加载 %s（%d 个事件）", filepath.Base(path), len(s.Events))
	for i, p := range a.scripts {
		if p == path {
			a.sel = i
		}
	}
	d.Invalidate()
}
