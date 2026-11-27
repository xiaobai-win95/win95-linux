package media

import (
	"encoding/binary"
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

func mp4Box(kind string, payload []byte) []byte {
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], kind)
	copy(box[8:], payload)
	return box
}

func writeMP4(cmd *exec.Cmd) {
	file, err := os.OpenFile(cmd.Args[len(cmd.Args)-1], os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	data := mp4Box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2"))
	data = append(data, mp4Box("mdat", []byte("video"))...)
	data = append(data, mp4Box("moov", []byte("metadata"))...)
	_, _ = file.Write(data)
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

func TestValidMP4RequiresCompleteTopLevelBoxes(t *testing.T) {
	ftyp := mp4Box("ftyp", []byte("isom"))
	mdat := mp4Box("mdat", []byte("video"))
	moov := mp4Box("moov", []byte("metadata"))
	extended := make([]byte, 16+len(mdat))
	binary.BigEndian.PutUint32(extended[:4], 1)
	copy(extended[4:8], "mdat")
	binary.BigEndian.PutUint64(extended[8:16], uint64(len(extended)))
	copy(extended[16:], mdat[8:])
	toEOF := append([]byte(nil), mdat...)
	binary.BigEndian.PutUint32(toEOF[:4], 0)
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"complete", append(append(append([]byte(nil), ftyp...), mdat...), moov...), true},
		{"extended size", append(append(append([]byte(nil), ftyp...), extended...), moov...), true},
		{"size to EOF", append(append(append([]byte(nil), ftyp...), moov...), toEOF...), true},
		{"missing moov", append(append([]byte(nil), ftyp...), mdat...), false},
		{"truncated box", append(append([]byte(nil), ftyp...), mdat[:len(mdat)-1]...), false},
		{"declared size beyond EOF", append(append([]byte(nil), ftyp...), mdat[:8]...), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.mp4")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if got := validMP4(path); got != test.want {
				t.Fatalf("validMP4 = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRecorderForceKillIsNeverSuccess(t *testing.T) {
	config := testConfig(t)
	process := &fakeProcess{waitCh: make(chan error, 1)}
	process.onSig = func(sig os.Signal) {
		if sig == syscall.SIGKILL {
			writeMP4(process.command)
			process.finish(errors.New("killed"))
		}
	}
	config.Start = func(cmd *exec.Cmd) (Process, error) {
		process.command = cmd
		return process, nil
	}
	recorder, err := NewRecorder(config)
	if err != nil {
		t.Fatal(err)
	}
	recorder.interruptTimeout = time.Millisecond
	recorder.terminateTimeout = time.Millisecond
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	recorder.Stop()
	if err := recorder.Shutdown(); err == nil || !strings.Contains(err.Error(), "force-killed") {
		t.Fatalf("Shutdown error = %v, want force-kill failure", err)
	}
}
