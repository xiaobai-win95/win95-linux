package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestDesktopRightClickOpensContextMenu(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 3, X: 400, Y: 300})
	if d.ctx == nil {
		t.Fatal("right-click on the desktop did not open a context menu")
	}
	if got := len(d.ctx.items); got == 0 {
		t.Fatal("context menu has no items")
	}
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 3, X: 400, Y: 300})
	d.Key(KeyEvent{Name: "Esc"})
	if d.ctx != nil {
		t.Fatal("Esc did not close the context menu")
	}
}

func TestIconRightClickAndKeyboardNavigation(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	x, y := d.iconPos(0)
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 3, X: x + 40, Y: y + 30})
	if d.ctx == nil {
		t.Fatal("right-click on an icon did not open a context menu")
	}
	if d.selected != 0 {
		t.Fatalf("right-click selected icon %d, want 0", d.selected)
	}
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 3, X: x + 40, Y: y + 30})

	d.openContext(d.desktopMenuItems(), 300, 300)
	m := d.ctx
	d.Key(KeyEvent{Name: "Down"})
	if m.hover != 0 {
		t.Fatalf("first Down selected item %d, want 0", m.hover)
	}
	d.Key(KeyEvent{Name: "Right"})
	if m.sub != 0 {
		t.Fatalf("Right did not open the submenu (sub=%d)", m.sub)
	}
	if m.subHover != 0 {
		t.Fatalf("submenu initial selection = %d, want 0", m.subHover)
	}
	d.Key(KeyEvent{Name: "Down"})
	if m.subHover != 3 {
		t.Fatalf("submenu Down skipped to %d, want 3 (disabled row skipped)", m.subHover)
	}
	d.Key(KeyEvent{Name: "Esc"})
	if m.sub != -1 {
		t.Fatal("Esc did not close the submenu")
	}
	d.Key(KeyEvent{Name: "Esc"})
	if d.ctx != nil {
		t.Fatal("Esc did not close the menu")
	}
}

func TestAltSpaceOpensWindowMenu(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.Open(NewCalcApp())
	d.Key(KeyEvent{Name: " ", Alt: true})
	if d.ctx == nil {
		t.Fatal("Alt+Space did not open the window system menu")
	}
	d.Key(KeyEvent{Name: "Esc"})
}

func TestManualIconPositionsDoNotOverlap(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.autoArrange = false
	d.iconPositions = map[string]iconPoint{}
	seen := map[iconPoint]bool{}
	for i := range d.icons {
		x, y := d.iconPos(i)
		p := iconPoint{x, y}
		if seen[p] {
			t.Fatalf("icon %d shares position %+v", i, p)
		}
		seen[p] = true
	}
}

func TestUniqueDesktopName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "新建文件夹"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniqueDesktopName(dir, "新建文件夹", ""); got != "新建文件夹 (2)" {
		t.Fatalf("unique name = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "新建文本文档.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniqueDesktopName(dir, "新建文本文档", ".txt"); got != "新建文本文档 (2).txt" {
		t.Fatalf("unique text name = %q", got)
	}
}

func TestSleepConfirmSuspendsAndWakes(t *testing.T) {
	called := false
	d := New(1024, 768, ModeDesktop, Host{Suspend: func() error {
		called = true
		return nil
	}})
	canvas := ui.NewCanvas(1024, 768)
	d.showSleep()
	d.Draw(canvas)
	if d.modal == nil {
		t.Fatal("sleep confirmation did not open")
	}
	dlg, ok := d.modal.App.(*dialogApp)
	if !ok || len(dlg.rects) == 0 {
		t.Fatalf("unexpected sleep dialog %T", d.modal.App)
	}
	r := dlg.rects[0]
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	if !d.sleepPending {
		t.Fatal("confirming sleep did not queue suspend")
	}
	d.Draw(canvas)
	if !d.sleeping && !d.sleepPending {
		t.Fatal("sleep screen not armed")
	}
	d.Animate(time.Now())
	if !called {
		t.Fatal("Host.Suspend was not called")
	}
	if d.sleepPending || d.sleeping {
		t.Fatal("desktop stayed in sleep state after resume")
	}
	if d.modal == nil {
		t.Fatal("wake notice did not open")
	}
	if a, ok := d.modal.App.(*dialogApp); !ok || a.title != "睡眠" {
		t.Fatalf("unexpected wake notice: %T", d.modal.App)
	}
}
