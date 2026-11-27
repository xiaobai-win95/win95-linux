package desktop

import (
	"errors"
	"path/filepath"
	"testing"

	"win95linux/internal/ui"
)

func TestDisplayRotationCoordinateRoundTrip(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		mode := DisplayMode{Width: 800, Height: 600, Rotation: rotation}
		w, h := mode.LogicalSize()
		if rotation%180 == 0 && (w != 800 || h != 600) {
			t.Fatalf("rotation %d size = %dx%d", rotation, w, h)
		}
		if rotation%180 != 0 && (w != 600 || h != 800) {
			t.Fatalf("rotation %d size = %dx%d", rotation, w, h)
		}
		for _, p := range [][2]int{{0, 0}, {37, 91}, {w - 1, h - 1}} {
			px, py := mode.PhysicalPoint(p[0], p[1], 800, 600)
			if px < 0 || px >= 800 || py < 0 || py >= 600 {
				t.Fatalf("rotation %d mapped %v outside display: %d,%d", rotation, p, px, py)
			}
			lx, ly := mode.LogicalPoint(px, py, 800, 600)
			if lx != p[0] || ly != p[1] {
				t.Fatalf("rotation %d round trip %v -> %d,%d -> %d,%d", rotation, p, px, py, lx, ly)
			}
		}
	}
}

func TestApplyDisplayModeResizesAndPersists(t *testing.T) {
	d := testDesktop(t)
	w := d.Open(&wsTestApp{})
	w.X, w.Y = 700, 500
	var requested DisplayMode
	d.Host.SetDisplayMode = func(mode DisplayMode) (DisplayMode, error) {
		requested = mode
		return mode, nil
	}
	mode := DisplayMode{Width: 1280, Height: 720, Refresh: 75, Rotation: 90}
	if err := d.applyDisplayMode(mode, false); err != nil {
		t.Fatal(err)
	}
	if requested != mode || d.DisplayMode() != mode {
		t.Fatalf("requested=%+v current=%+v", requested, d.DisplayMode())
	}
	if d.W != 720 || d.H != 1280 {
		t.Fatalf("logical desktop = %dx%d", d.W, d.H)
	}
	if w.X < 0 || w.Y < 0 || w.X+w.W > d.W || w.Y+w.H > d.taskbarTop() {
		t.Fatalf("window was not clamped: %+v", w)
	}
}

func TestApplyDisplayModeFailureKeepsCurrentMode(t *testing.T) {
	d := testDesktop(t)
	before := d.DisplayMode()
	d.Host.SetDisplayMode = func(DisplayMode) (DisplayMode, error) {
		return before, errors.New("driver rejected mode")
	}
	if err := d.applyDisplayMode(DisplayMode{Width: 1920, Height: 1080, Refresh: 75}, false); err == nil {
		t.Fatal("expected driver error")
	}
	if d.DisplayMode() != before || d.W != 800 || d.H != 600 {
		t.Fatalf("failed mode changed desktop: %+v %dx%d", d.DisplayMode(), d.W, d.H)
	}
}

func TestDisplayMonitorTabAndPinyinInput(t *testing.T) {
	d := testDesktop(t)
	d.Host.AvailableDisplays = func() []DisplayMode {
		return []DisplayMode{{Width: 800, Height: 600, Refresh: 60}, {Width: 1024, Height: 768, Refresh: 60}}
	}
	d.openDisplayProperties()
	display := d.windows[len(d.windows)-1].App.(*DisplayApp)
	display.tab = 2
	d.Draw(ui.NewCanvas(d.W, d.H))
	if len(display.tabs) != 3 || display.resolutionR.W == 0 || display.refreshR.W == 0 || display.rotateR.W == 0 {
		t.Fatal("monitor controls were not laid out")
	}
	d.CloseWindowForced(d.windows[len(d.windows)-1])

	w := d.InputBox("中文输入", "请输入", "", nil)
	d.Draw(ui.NewCanvas(d.W, d.H))
	input := w.App.(*InputBoxApp)
	d.Key(KeyEvent{Ctrl: true, Shift: true, Name: "Space", Rune: ' '})
	if !d.ime.enabled {
		t.Fatal("Ctrl+Shift+Space did not enable pinyin")
	}
	for _, r := range "nihao" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	if !d.imeComposing() {
		t.Fatal("pinyin composition was not started")
	}
	d.Key(KeyEvent{Name: "Space", Rune: ' '})
	if got := string(input.value); got != "你好" {
		t.Fatalf("committed input = %q", got)
	}

	for _, r := range "zhong" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	d.Key(KeyEvent{Name: "2", Rune: '2'})
	if got := string(input.value); got != "你好种" {
		t.Fatalf("numeric candidate input = %q", got)
	}
}

func TestDisplayMonitorTabFitsPortraitDesktop(t *testing.T) {
	d := testDesktop(t)
	d.displayMode = DisplayMode{Width: 640, Height: 480, Refresh: 60, Rotation: 90}
	d.ResizeDisplay(d.displayMode.LogicalSize())
	d.openDisplayProperties()
	w := d.windows[len(d.windows)-1]
	display := w.App.(*DisplayApp)
	display.tab = 2
	d.Draw(ui.NewCanvas(d.W, d.H))
	client := w.ClientRect()
	for _, r := range []ui.Rect{display.resolutionR, display.refreshR, display.rotateR} {
		if r.W <= 0 || r.X < client.X || r.Right() > client.Right() {
			t.Fatalf("monitor control %+v does not fit client %+v", r, client)
		}
	}
}

func TestDisplayResolutionDropdownScrolls(t *testing.T) {
	d := testDesktop(t)
	modes := make([]DisplayMode, 30)
	for i := range modes {
		modes[i] = DisplayMode{Width: 640 + i*16, Height: 480, Refresh: 60}
	}
	d.Host.AvailableDisplays = func() []DisplayMode { return modes }
	d.openDisplayProperties()
	w := d.windows[len(d.windows)-1]
	display := w.App.(*DisplayApp)
	display.tab = 2
	total := len(display.modes)
	display.modeIndex = total - 6
	display.openCombo(3)
	d.Draw(ui.NewCanvas(d.W, d.H))
	if display.comboScroll.Total != total || display.comboScroll.Visible >= total {
		t.Fatalf("dropdown scroll state = %+v", display.comboScroll)
	}
	if display.modeIndex < display.comboScroll.Pos || display.modeIndex >= display.comboScroll.Pos+display.comboScroll.Visible {
		t.Fatal("selected resolution is not visible")
	}
	client := w.ClientRect()
	lr := display.comboListRect(client, total)
	if lr.Bottom() > client.Bottom()-40 {
		t.Fatalf("dropdown %+v overlaps buttons in client %+v", lr, client)
	}
	if display.comboScroll.Pos != display.comboScroll.Max() {
		t.Fatalf("selected resolution did not scroll to bottom: Pos=%d Max=%d", display.comboScroll.Pos, display.comboScroll.Max())
	}
	display.Mouse(d, w, client, MouseEvent{Kind: MouseWheel, X: lr.X + 20, Y: lr.Y + 10, Wheel: -1})
	if display.comboScroll.Pos != display.comboScroll.Max() {
		t.Fatal("scrolling below the last resolution changed the dropdown position")
	}
	before := display.comboScroll.Pos
	display.Mouse(d, w, client, MouseEvent{Kind: MouseWheel, X: lr.X + 20, Y: lr.Y + 10, Wheel: 1})
	if display.comboScroll.Pos != before-1 {
		t.Fatalf("wheel scroll = %d, want %d", display.comboScroll.Pos, before-1)
	}
	want := display.comboScroll.Pos + display.comboScroll.Visible - 1
	y := lr.Y + 3 + (display.comboScroll.Visible-1)*20 + 10
	if got := display.comboIndexAt(lr.X+20, y); got != want {
		t.Fatalf("last visible row = %d, want %d", got, want)
	}
	display.Mouse(d, w, client, MouseEvent{Kind: MouseDown, Button: 1, X: lr.X + 20, Y: y})
	if display.comboOpen != 0 || display.modeIndex != want {
		t.Fatalf("selected mode = %d, want %d (combo %d)", display.modeIndex, want, display.comboOpen)
	}
}

func TestV266ProgramsExposeNewTools(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	entries := d.programEntries()
	found := make(map[string]bool)
	for _, entry := range entries {
		found[entry.id] = true
	}
	for _, id := range []string{"agent", "archive", "geekmonitor", "packetsniff", "diskmanager", "snapshots"} {
		if !found[id] {
			t.Errorf("programs menu is missing %q", id)
		}
	}
}

func TestAIPinyinInput(t *testing.T) {
	oldPath := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	t.Cleanup(func() { aiConfigPath = oldPath })
	d := testDesktop(t)
	a := d.openAI(0)
	w := d.windows[len(d.windows)-1]
	d.Draw(ui.NewCanvas(d.W, d.H))
	d.Key(KeyEvent{Ctrl: true, Shift: true, Name: "Space", Rune: ' '})
	for _, r := range "nihao" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	d.Key(KeyEvent{Name: "Space", Rune: ' '})
	if got := string(a.prompt); got != "你好" {
		t.Fatalf("AI prompt = %q, want 你好", got)
	}
	if d.ime.owner != w {
		t.Fatal("AI prompt did not retain IME focus")
	}

	a.tab, a.focus = 1, 1
	d.Draw(ui.NewCanvas(d.W, d.H))
	for _, r := range "zhongwen" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	d.Key(KeyEvent{Name: "Enter"})
	if got := string(a.chatInput); got != "中文" {
		t.Fatalf("AI chat input = %q, want 中文", got)
	}
	if len(a.chat) != 1 || a.chatBusy {
		t.Fatal("committing pinyin unexpectedly sent the chat")
	}
}

func TestAgentStudioPinyinInput(t *testing.T) {
	d, a, _ := newAgentStudioTest(t)
	a.win = d.Open(a)
	d.Draw(ui.NewCanvas(d.W, d.H))
	d.Key(KeyEvent{Ctrl: true, Shift: true, Name: "Space", Rune: ' '})
	for _, r := range "nihao" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	d.Key(KeyEvent{Name: "Space", Rune: ' '})
	if got := string(a.task); got != "你好" {
		t.Fatalf("Agent Studio task = %q, want 你好", got)
	}
	if a.taskCaret != len([]rune(a.task)) || d.ime.owner != a.win {
		t.Fatalf("task caret/IME owner = %d/%p", a.taskCaret, d.ime.owner)
	}
}

func TestNotepadPinyinInsertion(t *testing.T) {
	d := testDesktop(t)
	a := NewNotepadApp(d, "")
	w := d.Open(a)
	d.Draw(ui.NewCanvas(d.W, d.H))
	d.Key(KeyEvent{Ctrl: true, Shift: true, Name: "Space", Rune: ' '})
	for _, r := range "zhongwen" {
		d.Key(KeyEvent{Name: string(r), Rune: r})
	}
	d.Key(KeyEvent{Name: "Enter"})
	if a.lines[0] != "中文" {
		t.Fatalf("notepad content = %q", a.lines[0])
	}
	if d.ime.owner != w {
		t.Fatal("notepad did not retain IME focus")
	}
}
