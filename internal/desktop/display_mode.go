package desktop

import (
	"fmt"

	"win95linux/internal/ui"
)

func (d *Desktop) DisplayMode() DisplayMode { return d.displayMode }

func (d *Desktop) availableDisplayModes() []DisplayMode {
	if d.Host.AvailableDisplays != nil {
		return d.Host.AvailableDisplays()
	}
	return []DisplayMode{d.displayMode}
}

func (d *Desktop) applyDisplayMode(mode DisplayMode, confirm bool) error {
	mode = mode.Normalized()
	if mode == d.displayMode {
		return nil
	}
	if d.Host.SetDisplayMode == nil {
		return fmt.Errorf("%s", d.tr("当前显示驱动不支持动态切换模式"))
	}
	if confirm {
		d.MessageBox(d.tr("确认显示设置"),
			d.trf("将切换到 %d×%d、%d Hz、旋转 %d°。\n\n窗口会保留并自动调整位置；若显卡拒绝该模式，将恢复当前设置。", mode.Width, mode.Height, mode.Refresh, mode.Rotation),
			IconQuestion, []string{d.tr("应用"), d.tr("取消")}, func(i int) {
				if i == 0 {
					if err := d.applyDisplayMode(mode, false); err != nil {
						d.MessageBox(d.tr("显示设置"), err.Error(), IconError, []string{d.tr("确定")}, nil)
					}
				}
			})
		return nil
	}
	effective, err := d.Host.SetDisplayMode(mode)
	if err != nil {
		return fmt.Errorf("%s%w", d.tr("显示驱动未接受该模式，已保留当前设置："), err)
	}
	if effective.Width <= 0 || effective.Height <= 0 {
		effective = mode
	}
	d.displayMode = effective.Normalized()
	d.displayRequested = d.displayMode
	d.displayRequestedSet = true
	d.ResizeDisplay(d.displayMode.LogicalSize())
	d.savePrefs()
	for _, w := range d.windows {
		if a, ok := w.App.(*DisplayApp); ok {
			a.syncDisplayMode()
		}
	}
	d.Invalidate()
	return nil
}

func (d *Desktop) ResizeDisplay(width, height int) {
	if width < 640 || height < 480 {
		return
	}
	d.W, d.H = width, height
	d.MouseX = maxInt(0, minInt(d.MouseX, width-1))
	d.MouseY = maxInt(0, minInt(d.MouseY, height-1))
	d.startOpen = false
	d.startSub = ""
	d.ctx = nil
	d.capture = nil
	d.mouseDown = false
	if d.spotlight != nil {
		d.spotlight.Close()
	}
	maxW, maxH := width-16, d.taskbarTop()-16
	windows := append([]*Window(nil), d.windows...)
	for _, w := range windows {
		if w.Maximized {
			w.X, w.Y, w.W, w.H = 0, 0, width, d.taskbarTop()
		} else {
			w.W = maxInt(1, minInt(w.W, maxW))
			w.H = maxInt(1, minInt(w.H, maxH))
			w.X = maxInt(0, minInt(w.X, width-w.W))
			w.Y = maxInt(0, minInt(w.Y, d.taskbarTop()-w.H))
		}
		if client := w.ClientRect(); client.W > 0 && client.H > 0 {
			w.App.Resized(d, w, client)
		}
	}
	if !d.autoArrange {
		for _, ic := range d.icons {
			p := d.iconPositions[ic.key]
			p.x = maxInt(0, minInt(p.x, width-88))
			p.y = maxInt(0, minInt(p.y, d.taskbarTop()-78))
			d.iconPositions[ic.key] = p
		}
	}
	d.Invalidate()
}

func displayRotationLabel(rotation int) string {
	switch rotation {
	case 90:
		return "90°（顺时针）"
	case 180:
		return "180°"
	case 270:
		return "270°（逆时针）"
	default:
		return "0°（横向）"
	}
}

func drawMonitorPreview(c *ui.Canvas, r ui.Rect, mode DisplayMode) {
	c.FillRect(r, 0x808080)
	c.Raised(r)
	availW, availH := r.W-20, r.H-42
	logicalW, logicalH := mode.LogicalSize()
	screenW, screenH := availW, availH
	if screenW*logicalH > availH*logicalW {
		screenW = availH * logicalW / logicalH
	} else {
		screenH = screenW * logicalH / logicalW
	}
	screenW, screenH = maxInt(12, screenW), maxInt(12, screenH)
	screen := ui.Rect{r.X + (r.W-screenW)/2, r.Y + 10, screenW, screenH}
	drawBackgroundRect(c, screen, 0)
	c.Sunken(screen)
	standW := maxInt(12, r.W/5)
	c.FillRect(ui.Rect{r.X + (r.W-standW)/2, r.Bottom(), standW, 10}, ui.ColFace)
	c.Raised(ui.Rect{r.X + (r.W-standW)/2, r.Bottom(), standW, 10})
	baseW := maxInt(20, r.W/3)
	c.FillRect(ui.Rect{r.X + (r.W-baseW)/2, r.Bottom() + 8, baseW, 5}, ui.ColFace)
	c.Raised(ui.Rect{r.X + (r.W-baseW)/2, r.Bottom() + 8, baseW, 5})
}
