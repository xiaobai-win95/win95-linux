package desktop

import (
	"os"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func findSettings(t *testing.T, d *Desktop) (*SettingsApp, *Window) {
	t.Helper()
	d.openSettings()
	for _, w := range d.windows {
		if a, ok := w.App.(*SettingsApp); ok {
			return a, w
		}
	}
	t.Fatal("settings window not found")
	return nil, nil
}

func clickRect(d *Desktop, a *SettingsApp, w *Window, client ui.Rect, r ui.Rect) {
	a.Mouse(d, w, client, MouseEvent{Kind: MouseDown, Button: 1, X: r.X + 5, Y: r.Y + 5})
	a.Mouse(d, w, client, MouseEvent{Kind: MouseUp, Button: 1, X: r.X + 5, Y: r.Y + 5})
}

func TestSettingsPagesRenderAndNavigate(t *testing.T) {
	d := testDesktop(t)
	a, w := findSettings(t, d)
	c := ui.NewCanvas(w.W, w.H)
	client := ui.Rect{0, 0, w.W, w.H}

	for p := settingsPage(0); p < settingsPageCount; p++ {
		a.page = p
		a.Paint(d, w, c, client)
		if len(a.navRects) != int(settingsPageCount) {
			t.Fatalf("page %d: nav rects = %d, want %d", p, len(a.navRects), settingsPageCount)
		}
	}

	a.page = settingsLanguage
	a.Paint(d, w, c, client)
	r := a.navRects[int(settingsTheme)]
	a.Mouse(d, w, client, MouseEvent{Kind: MouseDown, Button: 1, X: r.X + 5, Y: r.Y + 5})
	if a.page != settingsTheme {
		t.Fatalf("nav click page = %v, want theme", a.page)
	}

	if !a.Key(d, w, KeyEvent{Name: "Right"}) || a.page != settingsTime {
		t.Fatalf("Right key page = %v, want time", a.page)
	}
	if !a.Key(d, w, KeyEvent{Name: "Esc"}) {
		t.Fatal("Esc should close settings")
	}
	for _, win := range d.windows {
		if win == w {
			t.Fatal("settings window still open after Esc")
		}
	}
}

func TestSettingsLanguageThemeTimeApply(t *testing.T) {
	d := testDesktop(t)
	a, w := findSettings(t, d)
	c := ui.NewCanvas(w.W, w.H)
	client := ui.Rect{0, 0, w.W, w.H}

	a.page = settingsLanguage
	a.Paint(d, w, c, client)
	en := -1
	for i, l := range desktopLanguages {
		if l.Code == "en-US" {
			en = i
			break
		}
	}
	if en < 0 {
		t.Fatal("en-US missing from language list")
	}
	clickRect(d, a, w, client, a.langRects[en])
	if d.lang != "en-US" {
		t.Fatalf("lang = %q, want en-US", d.lang)
	}
	if got := d.tr("开始"); got != "Start" {
		t.Fatalf("tr(开始) = %q, want Start", got)
	}

	a.page = settingsTheme
	a.Paint(d, w, c, client)
	clickRect(d, a, w, client, a.themeRects[2])
	if d.theme != 2 {
		t.Fatalf("theme = %d, want 2", d.theme)
	}

	if _, err := time.LoadLocation("Pacific/Auckland"); err != nil {
		t.Skip("tzdata unavailable")
	}
	a.page = settingsTime
	a.Paint(d, w, c, client)
	first := ui.Rect{a.zoneBox.X + 4, a.zoneBox.Y + 3, 40, 18}
	a.Mouse(d, w, client, MouseEvent{Kind: MouseDown, Button: 1, X: first.X, Y: first.Y})
	if d.clockZone != "Pacific/Auckland" {
		t.Fatalf("clock zone = %q, want Pacific/Auckland", d.clockZone)
	}

	a.page = settingsRegion
	a.Paint(d, w, c, client)
	row := regionPresets[5]
	clickRect(d, a, w, client, ui.Rect{a.regionBox.X + 4, a.regionBox.Y + 3 + 5*26, 40, 20})
	if d.clockZone != row.Zone || d.lang != row.Lang {
		t.Fatalf("region apply = %s/%s, want %s/%s", d.lang, d.clockZone, row.Lang, row.Zone)
	}
	if !d.clock12 {
		t.Fatal("US region should enable 12-hour clock")
	}

	data, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("prefs not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("prefs file empty")
	}
}

func TestI18nFallback(t *testing.T) {
	d := testDesktop(t)
	d.lang = "fr-FR"
	if got := d.tr("开始"); got != "Démarrer" {
		t.Fatalf("fr tr(开始) = %q", got)
	}
	if got := d.tr("三维弹球"); got == "三维弹球" {
		t.Fatal("pinball label not translated for fr-FR")
	}
	d.lang = "xx-XX"
	if got := d.tr("开始"); got != "Start" {
		t.Fatalf("unknown language fallback = %q, want Start", got)
	}
	if got := d.tr("完全不存在的键"); got != "完全不存在的键" {
		t.Fatalf("missing key = %q, want source text", got)
	}
	d.lang = "zh-CN"
	if got := d.tr("开始"); got != "开始" {
		t.Fatalf("zh-CN tr(开始) = %q", got)
	}
}

func TestPinballLaunchesAndRenders(t *testing.T) {
	d := testDesktop(t)
	d.openPinball()
	var a *PinballApp
	var w *Window
	for _, win := range d.windows {
		if p, ok := win.App.(*PinballApp); ok {
			a, w = p, win
			break
		}
	}
	if a == nil {
		t.Fatal("pinball window not opened")
	}
	if a.balls != 3 || a.gameOver {
		t.Fatalf("new game: balls=%d over=%v", a.balls, a.gameOver)
	}
	c := ui.NewCanvas(w.W, w.H)
	client := ui.Rect{0, 0, w.W, w.H}
	a.Paint(d, w, c, client)

	a.launch()
	if !a.launched {
		t.Fatal("ball not launched")
	}
	start := time.Now()
	for i := 1; i <= 900; i++ {
		a.Animate(d, w, start.Add(time.Duration(i)*16*time.Millisecond))
		if a.ball.x < pinLeft-pinBallR || a.ball.x > pinRight+pinBallR {
			t.Fatalf("ball escaped table at step %d: %.1f", i, a.ball.x)
		}
		if a.ball.y < pinTop-40 || a.ball.y > pinTableH {
			t.Fatalf("ball escaped vertically at step %d: %.1f", i, a.ball.y)
		}
	}
	if a.balls < 0 || a.balls > 3 {
		t.Fatalf("balls = %d", a.balls)
	}
	if a.score < 0 {
		t.Fatalf("score = %d", a.score)
	}

	a.newGame()
	if a.balls != 3 || a.gameOver || a.score != 0 {
		t.Fatalf("restart: balls=%d over=%v score=%d", a.balls, a.gameOver, a.score)
	}
	for i := 0; i < 4; i++ {
		a.drain()
	}
	if !a.gameOver {
		t.Fatal("game should be over after draining all balls")
	}
	if !a.Key(d, w, KeyEvent{Name: "N"}) {
		t.Fatal("N should restart")
	}
	if a.balls != 3 || a.gameOver {
		t.Fatal("N did not restart the game")
	}
}
