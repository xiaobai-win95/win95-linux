package desktop

import (
	"testing"

	"win95linux/internal/ui"
)

func TestModalButtonClickUsesScreenCoordinates(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	canvas := ui.NewCanvas(1024, 768)
	clicked := -1
	d.MessageBox("测试", "点击确定。", IconInfo, []string{"确定"}, func(i int) { clicked = i })
	d.Draw(canvas)

	if d.modal == nil {
		t.Fatal("对话框未打开")
	}
	a, ok := d.modal.App.(*dialogApp)
	if !ok {
		t.Fatalf("对话框应用类型异常: %T", d.modal.App)
	}
	if len(a.rects) != 1 {
		t.Fatalf("按钮数量 = %d, 期望 1", len(a.rects))
	}
	r := a.rects[0]
	x, y := r.X+r.W/2, r.Y+r.H/2
	d.Mouse(MouseEvent{Kind: MouseDown, X: x, Y: y, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: x, Y: y, Button: 1})
	if clicked != 0 {
		t.Fatalf("按钮未响应屏幕坐标点击: clicked=%d", clicked)
	}
	if d.modal != nil {
		t.Fatal("点击确定后对话框未关闭")
	}
}

func TestInstallerButtonsUseScreenCoordinates(t *testing.T) {
	d := New(1024, 768, ModeInstaller, Host{})
	canvas := ui.NewCanvas(1024, 768)
	a := d.installer
	a.disks = []diskInfo{
		{path: "/dev/vda", sizeBytes: 64 << 30},
		{path: "/dev/vdb", sizeBytes: 10 << 30},
	}
	a.sel = 0
	a.scroll.Total = len(a.disks)
	d.Draw(canvas)

	row := a.rows[1]
	d.Mouse(MouseEvent{Kind: MouseDown, X: row.X + row.W/2, Y: row.Y + row.H/2, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: row.X + row.W/2, Y: row.Y + row.H/2, Button: 1})
	if a.sel != 1 {
		t.Fatalf("点击磁盘行后 sel=%d, 期望 1", a.sel)
	}

	start := a.btnStart
	d.Mouse(MouseEvent{Kind: MouseDown, X: start.X + start.W/2, Y: start.Y + start.H/2, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: start.X + start.W/2, Y: start.Y + start.H/2, Button: 1})
	if d.modal == nil {
		t.Fatal("点击“开始安装”后未出现确认对话框")
	}

	d.Draw(canvas)
	dlg, ok := d.modal.App.(*dialogApp)
	if !ok {
		t.Fatalf("确认框应用类型异常: %T", d.modal.App)
	}
	if len(dlg.rects) != 2 {
		t.Fatalf("确认框按钮数量 = %d, 期望 2", len(dlg.rects))
	}
	cancel := dlg.rects[1]
	d.Mouse(MouseEvent{Kind: MouseDown, X: cancel.X + cancel.W/2, Y: cancel.Y + cancel.H/2, Button: 1})
	d.Mouse(MouseEvent{Kind: MouseUp, X: cancel.X + cancel.W/2, Y: cancel.Y + cancel.H/2, Button: 1})
	if d.modal != nil {
		t.Fatal("点击“取消”后确认框未关闭")
	}
}
