package media

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeProcess struct {
	mu      sync.Mutex
	waitCh  chan error
	signals []os.Signal
	once    sync.Once
	onSig   func(os.Signal)
	command *exec.Cmd
}

func (p *fakeProcess) Signal(sig os.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, sig)
	onSig := p.onSig
	p.mu.Unlock()
	if onSig != nil {
		onSig(sig)
	}
	return nil
}

func (p *fakeProcess) Wait() error {
	return <-p.waitCh
}

func (p *fakeProcess) finish(err error) {
	p.once.Do(func() { p.waitCh <- err })
}

func testConfig(t *testing.T) RecorderConfig {
	t.Helper()
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(dir, "fb0")
	if err := os.WriteFile(fb, []byte("framebuffer"), 0o600); err != nil {
		t.Fatal(err)
	}
	return RecorderConfig{
		FFmpegPath:  ffmpeg,
		Framebuffer: fb,
		OutputDir:   filepath.Join(dir, "out"),
		LogPath:     filepath.Join(dir, "recorder.log"),
		FPS:         15,
		MaxBytes:    32 << 20,
		Now:         func() time.Time { return time.Date(2026, 11, 27, 12, 34, 56, 0, time.UTC) },
		FreeSpace:   func(string) (uint64, error) { return 256 << 20, nil },
	}
}

func writeMP4(cmd *exec.Cmd) {
	file, err := os.OpenFile(cmd.Args[len(cmd.Args)-1], os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"))
	_ = file.Close()
}

func TestRecorderStartAndStop(t *testing.T) {
	config := testConfig(t)
	process := &fakeProcess{waitCh: make(chan error, 1)}
	process.onSig = func(sig os.Signal) {
		if sig == syscall.SIGINT {
			writeMP4(process.command)
			process.finish(errors.New("interrupted"))
		}
	}
	var command *exec.Cmd
	config.Start = func(cmd *exec.Cmd) (Process, error) {
		command = cmd
		process.command = cmd
		return process, nil
	}
	recorder, err := NewRecorder(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	if !recorder.Running() {
		t.Fatal("recorder did not enter running state")
	}
	joined := strings.Join(command.Args, " ")
	for _, want := range []string{"-f fbdev", "-framerate 15", "-c:v libx264", "-fs 33554432"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command %q does not contain %q", joined, want)
		}
	}
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatal("recorder must run in its own process group")
	}
	output := recorder.OutputPath()
	if !filepath.IsAbs(output) || !strings.HasSuffix(output, "screen-20261127-123456.mp4") {
		t.Fatalf("unexpected output path %q", output)
	}
	recorder.Stop()
	if err := recorder.Shutdown(); err != nil {
		t.Fatalf("clean stop was reported as failure: %v", err)
	}
	if recorder.Running() || !recorder.Finished() {
		t.Fatal("recorder did not finish")
	}
	if !validMP4(output) {
		t.Fatal("finalized recording is not an MP4")
	}
}

func TestRecorderRejectsLowSpace(t *testing.T) {
	config := testConfig(t)
	config.FreeSpace = func(string) (uint64, error) { return uint64(MinimumFreeBytes - 1), nil }
	recorder, err := NewRecorder(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Start(); err == nil || !strings.Contains(err.Error(), "free disk space") {
		t.Fatalf("Start error = %v, want free-space failure", err)
	}
}

func TestRecorderUsesUniqueOutput(t *testing.T) {
	config := testConfig(t)
	var paths []string
	config.Start = func(cmd *exec.Cmd) (Process, error) {
		paths = append(paths, cmd.Args[len(cmd.Args)-1])
		writeMP4(cmd)
		process := &fakeProcess{waitCh: make(chan error, 1)}
		process.finish(nil)
		return process, nil
	}
	for i := 0; i < 2; i++ {
		recorder, err := NewRecorder(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := recorder.Start(); err != nil {
			t.Fatal(err)
		}
		if err := recorder.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatalf("output paths are not unique: %v", paths)
	}
}

func TestRecorderReportsFailedOutput(t *testing.T) {
	config := testConfig(t)
	config.Start = func(cmd *exec.Cmd) (Process, error) {
		process := &fakeProcess{waitCh: make(chan error, 1)}
		process.finish(errors.New("encoder failed"))
		return process, nil
	}
	recorder, err := NewRecorder(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Shutdown(); err == nil {
		t.Fatal("failed recording was reported as successful")
	}
}
