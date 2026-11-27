package desktop

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestScreenRecorderPaintAndMenu(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := NewScreenRecorderApp(d)
	canvas := ui.NewCanvas(640, 470)
	a.Paint(d, nil, canvas, ui.Rect{0, 0, 640, 470})
	if a.btnStart.W == 0 || a.btnStop.W == 0 || a.preview.H == 0 {
		t.Fatalf("recorder layout is incomplete: %+v", a)
	}
	if len(a.Menus(d, nil)) != 2 {
		t.Fatal("recorder menus are missing")
	}
	a.state = screenRecorderFailed
	a.errMsg = "framebuffer unavailable"
	a.Paint(d, nil, canvas, ui.Rect{0, 0, 640, 470})
}

func TestMediaClassificationAndStartMenu(t *testing.T) {
	for _, name := range []string{"song.mp3", "movie.mp4", "capture.ts", "clip.WEBM"} {
		if kind, typ := fmClassify(name); kind != ui.IcoFile || typ != "媒体文件" {
			t.Errorf("fmClassify(%q) = %v, %q", name, kind, typ)
		}
	}
	d := New(1024, 768, ModeDesktop, Host{})
	found := map[string]bool{}
	for _, entry := range d.programEntries() {
		found[entry.id] = true
	}
	if !found["screenrecorder"] || !found["mediaplayer"] {
		t.Fatalf("media Start menu entries are missing: %v", found)
	}
}

func TestX11ProxyCloseStopsManagedMedia(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sleep, "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	d := New(800, 600, ModeDesktop, Host{})
	a := &X11ProxyApp{d: d, mediaCmd: cmd, mediaDone: done, mediaPath: "/root/movie.mp4"}
	a.Close()
	select {
	case <-exited:
	case <-time.After(4 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("managed media process survived proxy close")
	}
	if a.mediaPending != "" {
		t.Fatal("proxy close left queued playback behind")
	}
}

func TestRecordingDurationFormat(t *testing.T) {
	if got := formatRecordingDuration(3661 * time.Second); got != "01:01:01" {
		t.Fatalf("duration = %q", got)
	}
}
