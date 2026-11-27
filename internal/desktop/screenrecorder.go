package desktop

import (
	"fmt"
	"os"
	"strings"
	"time"

	"win95linux/internal/media"
	"win95linux/internal/ui"
)

const (
	screenRecorderIdle = iota
	screenRecorderRecording
	screenRecorderFinalizing
	screenRecorderDone
	screenRecorderFailed
)

type ScreenRecorderApp struct {
	Base
	d   *Desktop
	win *Window

	recorder       *media.Recorder
	state          int
	fps            int
	errMsg         string
	closeAfterStop bool
	lastBlink      time.Time
	blinkOn        bool
	press          string

	btnStart, btnStop, btnOpen, btnFolder ui.Rect
	fpsRects                              [3]ui.Rect
	preview                               ui.Rect
}

func NewScreenRecorderApp(d *Desktop) *ScreenRecorderApp {
	return &ScreenRecorderApp{d: d, fps: 15}
}

func (a *ScreenRecorderApp) Title() string           { return a.d.tr("屏幕录像机") }
func (a *ScreenRecorderApp) Icon() ui.IconKind       { return ui.IcoMacro }
func (a *ScreenRecorderApp) DefaultSize() (int, int) { return 640, 470 }
func (a *ScreenRecorderApp) MinSize() (int, int)     { return 520, 390 }
func (a *ScreenRecorderApp) Resizable() bool         { return true }

func (a *ScreenRecorderApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: d.tr("文件(F)"), Items: []MenuItem{
			{Label: d.tr("开始录制(F9)"), Accel: "F9", Enabled: a.state == screenRecorderIdle || a.state == screenRecorderDone || a.state == screenRecorderFailed, Action: a.start},
			{Label: d.tr("停止录制(F10)"), Accel: "F10", Enabled: a.state == screenRecorderRecording, Action: a.stop},
			{Separator: true},
			{Label: d.tr("打开录像(O)…"), Enabled: a.hasOutput(), Action: a.openOutput},
			{Label: d.tr("打开录像文件夹"), Enabled: true, Action: a.openFolder},
			{Separator: true},
			{Label: d.tr("关闭(C)"), Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: d.tr("帮助(H)"), Items: []MenuItem{
			{Label: d.tr("关于屏幕录像机(A)"), Enabled: true, Action: func() {
				d.MessageBox(a.Title(),
					d.tr("直接录制物理 framebuffer，分辨率和屏幕转向会保留在画面中。\n录像保存为 H.264 MP4，单文件上限 1 GiB。\n停止时会先正常收尾，必要时强制结束 FFmpeg。"),
					IconInfo, []string{d.tr("确定")}, nil)
			}},
		}},
	}
}

func (a *ScreenRecorderApp) layout(client ui.Rect) {
	ty := client.Y + 5
	tx := client.X + 5
	a.btnStart = ui.Rect{tx, ty, 96, 25}
	a.btnStop = ui.Rect{a.btnStart.Right() + 5, ty, 96, 25}
	a.btnOpen = ui.Rect{a.btnStop.Right() + 5, ty, 96, 25}
	a.btnFolder = ui.Rect{a.btnOpen.Right() + 5, ty, 112, 25}
	for i := range a.fpsRects {
		a.fpsRects[i] = ui.Rect{a.btnFolder.Right() + 8 + i*48, ty, 44, 25}
	}
	statusH := 24
	a.preview = ui.Rect{client.X + 5, client.Y + 38, client.W - 10, client.H - 38 - statusH - 5}
	if a.preview.H < 40 {
		a.preview.H = 40
	}
}

func (a *ScreenRecorderApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	a.win = w
	c.FillRect(client, ui.ColFace)
	a.layout(client)
	f := ui.DefaultFont()
	busy := a.state == screenRecorderRecording || a.state == screenRecorderFinalizing
	ui.Button(c, a.btnStart, d.tr("开始录制"), a.press == "start", !busy)
	ui.Button(c, a.btnStop, d.tr("停止录制"), a.press == "stop", a.state == screenRecorderRecording)
	ui.Button(c, a.btnOpen, d.tr("打开录像"), a.press == "open", a.hasOutput())
	ui.Button(c, a.btnFolder, d.tr("录像文件夹"), a.press == "folder", true)
	for i, fps := range []int{15, 24, 30} {
		ui.Button(c, a.fpsRects[i], fmt.Sprintf("%d", fps), a.press == fmt.Sprintf("fps%d", fps) || (!busy && a.fps == fps), !busy)
	}

	c.FillRect(a.preview, 0x101010)
	c.Sunken(a.preview)
	centerY := a.preview.Y + a.preview.H/2
	heading := d.tr("准备录制整个屏幕")
	detail := d.tr("选择帧率，然后点击“开始录制”。")
	headingColor := uint32(ui.ColWhite)
	if a.state == screenRecorderRecording {
		heading = d.tr("正在录制")
		detail = formatRecordingDuration(time.Since(a.recorder.StartedAt()))
		if a.blinkOn {
			c.FillRect(ui.Rect{a.preview.X + a.preview.W/2 - 76, centerY - 34, 12, 12}, 0xFF3030)
		}
	} else if a.state == screenRecorderFinalizing {
		heading = d.tr("正在停止并完成 MP4…")
		detail = d.tr("请稍候，不要关闭系统。")
	} else if a.state == screenRecorderDone {
		heading = d.tr("录制完成")
		detail = a.recorder.OutputPath()
	} else if a.state == screenRecorderFailed {
		heading = d.tr("录制失败")
		detail = a.errMsg
		headingColor = 0xFF8080
	}
	f.Text(c, a.preview.X+(a.preview.W-f.Width(heading))/2, centerY-24, heading, headingColor)
	f.TextClip(c, a.preview.X+16, centerY+4, a.preview.W-32, detail, ui.ColShadow)

	status := ui.Rect{client.X, client.Bottom() - 24, client.W, 24}
	c.FillRect(status, ui.ColFace)
	c.HLine(status.X, status.Y, status.W, ui.ColShadow)
	c.HLine(status.X, status.Y+1, status.W, ui.ColWhite)
	text := d.tr("输出目录：/root/录像 · H.264 MP4 · 上限 1 GiB")
	if a.recorder != nil && a.recorder.OutputPath() != "" {
		text = fmt.Sprintf("%s · %s", a.recorder.OutputPath(), formatBytes(a.recorder.OutputSize()))
	}
	f.TextClip(c, status.X+6, status.Y+5, status.W-190, text, ui.ColText)
	f.TextRight(c, status.Right()-6, status.Y+5, fmt.Sprintf("%d FPS", a.fps), ui.ColShadow)
}

func (a *ScreenRecorderApp) start() {
	if a.state == screenRecorderRecording || a.state == screenRecorderFinalizing {
		return
	}
	a.errMsg = ""
	a.closeAfterStop = false
	recorder, err := media.NewRecorder(media.RecorderConfig{FPS: a.fps})
	if err != nil {
		a.fail(err)
		return
	}
	if err := recorder.Start(); err != nil {
		a.fail(err)
		return
	}
	a.recorder = recorder
	a.state = screenRecorderRecording
	a.lastBlink = time.Now()
	a.d.Invalidate()
}

func (a *ScreenRecorderApp) fail(err error) {
	a.state = screenRecorderFailed
	a.errMsg = err.Error()
	if a.recorder != nil {
		if tail := a.recorder.LogTail(1200); tail != "" {
			a.errMsg += "\n" + tail
		}
	}
	a.d.Invalidate()
}

func (a *ScreenRecorderApp) stop() {
	if a.state != screenRecorderRecording || a.recorder == nil {
		return
	}
	a.state = screenRecorderFinalizing
	a.recorder.Stop()
	a.d.Invalidate()
}

func (a *ScreenRecorderApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if a.recorder != nil && !a.recorder.Finished() && now.Sub(a.lastBlink) >= 500*time.Millisecond {
		a.lastBlink = now
		a.blinkOn = !a.blinkOn
		d.Invalidate()
	}
	if a.recorder == nil || !a.recorder.Finished() ||
		(a.state != screenRecorderRecording && a.state != screenRecorderFinalizing) {
		return
	}
	if err := a.recorder.Error(); err != nil {
		a.state = screenRecorderFailed
		a.errMsg = err.Error()
		if tail := a.recorder.LogTail(1600); tail != "" {
			a.errMsg += "\n" + tail
		}
	} else {
		a.state = screenRecorderDone
	}
	d.Invalidate()
	if a.closeAfterStop && a.state != screenRecorderFailed {
		a.closeAfterStop = false
		d.CloseWindow(w)
	}
}

func (a *ScreenRecorderApp) hasOutput() bool {
	if a.recorder == nil {
		return false
	}
	path := a.recorder.OutputPath()
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func (a *ScreenRecorderApp) openOutput() {
	if !a.hasOutput() {
		return
	}
	a.d.openFileDefault(a.recorder.OutputPath())
}

func (a *ScreenRecorderApp) openFolder() {
	a.d.openFiles(media.DefaultOutputDir)
}

func (a *ScreenRecorderApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.layout(client)
	if ev.Kind == MouseDown {
		a.press = ""
		switch {
		case a.btnStart.Contains(ev.X, ev.Y):
			a.press = "start"
		case a.btnStop.Contains(ev.X, ev.Y):
			a.press = "stop"
		case a.btnOpen.Contains(ev.X, ev.Y):
			a.press = "open"
		case a.btnFolder.Contains(ev.X, ev.Y):
			a.press = "folder"
		default:
			for i := range a.fpsRects {
				if a.fpsRects[i].Contains(ev.X, ev.Y) {
					a.press = fmt.Sprintf("fps%d", i)
				}
			}
		}
		return
	}
	if ev.Kind != MouseUp {
		return
	}
	press := a.press
	a.press = ""
	switch press {
	case "start":
		if a.btnStart.Contains(ev.X, ev.Y) {
			a.start()
		}
	case "stop":
		if a.btnStop.Contains(ev.X, ev.Y) {
			a.stop()
		}
	case "open":
		if a.btnOpen.Contains(ev.X, ev.Y) {
			a.openOutput()
		}
	case "folder":
		if a.btnFolder.Contains(ev.X, ev.Y) {
			a.openFolder()
		}
	default:
		if strings.HasPrefix(press, "fps") && (a.state == screenRecorderIdle || a.state == screenRecorderDone || a.state == screenRecorderFailed) {
			for i, fps := range []int{15, 24, 30} {
				if press == fmt.Sprintf("fps%d", i) && a.fpsRects[i].Contains(ev.X, ev.Y) {
					a.fps = fps
					d.Invalidate()
				}
			}
		}
	}
}

func (a *ScreenRecorderApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "F9":
		a.start()
		return true
	case "F10":
		a.stop()
		return true
	}
	return false
}

func (a *ScreenRecorderApp) RequestClose(d *Desktop, w *Window) bool {
	if a.recorder == nil || !a.recorder.Running() {
		return true
	}
	if a.recorder.Stopping() {
		return false
	}
	d.MessageBox(d.tr("屏幕录像机"),
		d.tr("录制仍在进行中。要停止录制、完成 MP4 文件后关闭吗？"),
		IconWarning, []string{d.tr("停止并关闭"), d.tr("取消")}, func(i int) {
			if i != 0 {
				return
			}
			a.closeAfterStop = true
			a.stop()
		})
	return false
}

func (a *ScreenRecorderApp) Close() {
	if a.recorder != nil {
		_ = a.recorder.Shutdown()
	}
}

func (d *Desktop) openScreenRecorder() {
	for _, w := range d.windows {
		if _, ok := w.App.(*ScreenRecorderApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := NewScreenRecorderApp(d)
	w := d.Open(a)
	a.win = w
}

func formatRecordingDuration(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := int64(elapsed / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds/60)%60, seconds%60)
}
