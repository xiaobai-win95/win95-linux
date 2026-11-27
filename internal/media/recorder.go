package media

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultFFmpeg      = "/usr/bin/ffmpeg"
	DefaultFramebuffer = "/dev/fb0"
	DefaultOutputDir   = "/root/录像"
	DefaultLogPath     = "/var/log/win95-screen-recorder.log"
	DefaultMaxBytes    = int64(1 << 30)
	MinimumFreeBytes   = int64(128 << 20)
)

type Process interface {
	Signal(os.Signal) error
	Wait() error
}

type RecorderConfig struct {
	FFmpegPath  string
	Framebuffer string
	OutputDir   string
	LogPath     string
	FPS         int
	MaxBytes    int64
	Now         func() time.Time
	FreeSpace   func(string) (uint64, error)
	Start       func(*exec.Cmd) (Process, error)
}

type Recorder struct {
	mu            sync.Mutex
	config        RecorderConfig
	process       Process
	output        string
	started       time.Time
	stopRequested bool
	finished      bool
	result        error
	finishedCh    chan struct{}
	logFile       *os.File
}

func NewRecorder(config RecorderConfig) (*Recorder, error) {
	if config.FFmpegPath == "" {
		config.FFmpegPath = DefaultFFmpeg
	}
	if config.Framebuffer == "" {
		config.Framebuffer = DefaultFramebuffer
	}
	if config.OutputDir == "" {
		config.OutputDir = DefaultOutputDir
	}
	if config.LogPath == "" {
		config.LogPath = DefaultLogPath
	}
	if config.FPS == 0 {
		config.FPS = 15
	}
	if config.FPS < 1 || config.FPS > 60 {
		return nil, errors.New("frame rate must be between 1 and 60")
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = DefaultMaxBytes
	}
	if config.MaxBytes < 16<<20 {
		return nil, errors.New("recording size limit must be at least 16 MiB")
	}
	if !filepath.IsAbs(config.FFmpegPath) || !filepath.IsAbs(config.Framebuffer) ||
		!filepath.IsAbs(config.OutputDir) || !filepath.IsAbs(config.LogPath) {
		return nil, errors.New("media paths must be absolute")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.FreeSpace == nil {
		config.FreeSpace = freeSpace
	}
	if config.Start == nil {
		config.Start = startProcess
	}
	return &Recorder{config: config}, nil
}

func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.process != nil && !r.finished {
		return errors.New("recording is already running")
	}
	st, err := os.Stat(r.config.FFmpegPath)
	if err != nil {
		return fmt.Errorf("ffmpeg is unavailable: %w", err)
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return errors.New("ffmpeg is not executable")
	}
	if st, err := os.Stat(r.config.Framebuffer); err != nil ||
		(!st.Mode().IsRegular() && st.Mode()&os.ModeCharDevice == 0) {
		return fmt.Errorf("framebuffer is unavailable: %s", r.config.Framebuffer)
	}
	dir, err := prepareOutputDir(r.config.OutputDir)
	if err != nil {
		return err
	}
	free, err := r.config.FreeSpace(dir)
	if err != nil {
		return fmt.Errorf("cannot check free disk space: %w", err)
	}
	if free < uint64(MinimumFreeBytes) {
		return fmt.Errorf("at least %d MiB of free disk space is required", MinimumFreeBytes>>20)
	}
	output, file, err := reserveOutput(dir, r.config.Now())
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(output)
		return err
	}
	logFile, err := os.OpenFile(r.config.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = os.Remove(output)
		return fmt.Errorf("cannot open recorder log: %w", err)
	}
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-f", "fbdev", "-framerate", strconv.Itoa(r.config.FPS),
		"-i", r.config.Framebuffer,
		"-an", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p",
		"-threads", "2", "-movflags", "+faststart",
		"-fs", strconv.FormatInt(r.config.MaxBytes, 10), output,
	}
	cmd := exec.Command(r.config.FFmpegPath, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8"}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process, err := r.config.Start(cmd)
	if err != nil {
		_ = logFile.Close()
		_ = os.Remove(output)
		return fmt.Errorf("cannot start ffmpeg: %w", err)
	}
	r.process = process
	r.output = output
	r.started = r.config.Now()
	r.stopRequested = false
	r.finished = false
	r.result = nil
	r.finishedCh = make(chan struct{})
	r.logFile = logFile
	go r.wait(process)
	return nil
}

func (r *Recorder) wait(process Process) {
	err := process.Wait()
	valid := validMP4(r.output)
	r.mu.Lock()
	stopping := r.stopRequested
	if !valid {
		r.result = errors.New("ffmpeg did not produce a valid MP4 file")
	} else if err != nil && !stopping {
		r.result = fmt.Errorf("ffmpeg exited: %w", err)
	}
	r.finished = true
	r.process = nil
	if r.logFile != nil {
		_ = r.logFile.Close()
		r.logFile = nil
	}
	finishedCh := r.finishedCh
	r.mu.Unlock()
	close(finishedCh)
}

func (r *Recorder) Stop() {
	r.mu.Lock()
	if r.finished || r.stopRequested || r.process == nil {
		r.mu.Unlock()
		return
	}
	r.stopRequested = true
	process := r.process
	finishedCh := r.finishedCh
	r.mu.Unlock()
	_ = process.Signal(syscall.SIGINT)
	go func() {
		select {
		case <-finishedCh:
			return
		case <-time.After(3 * time.Second):
		}
		_ = process.Signal(syscall.SIGTERM)
		select {
		case <-finishedCh:
			return
		case <-time.After(2 * time.Second):
		}
		_ = process.Signal(syscall.SIGKILL)
	}()
}

func (r *Recorder) Shutdown() error {
	r.Stop()
	r.mu.Lock()
	finished := r.finished
	finishedCh := r.finishedCh
	r.mu.Unlock()
	if finished {
		return r.Error()
	}
	if finishedCh == nil {
		return nil
	}
	select {
	case <-finishedCh:
		return r.Error()
	case <-time.After(6 * time.Second):
		return errors.New("recorder did not stop within 6 seconds")
	}
}

func (r *Recorder) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.process != nil && !r.finished
}

func (r *Recorder) Stopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopRequested && !r.finished
}

func (r *Recorder) Finished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished
}

func (r *Recorder) Error() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

func (r *Recorder) OutputPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.output
}

func (r *Recorder) StartedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

func (r *Recorder) OutputSize() int64 {
	st, err := os.Stat(r.OutputPath())
	if err != nil {
		return 0
	}
	return st.Size()
}

func (r *Recorder) LogTail(limit int) string {
	data, err := os.ReadFile(r.config.LogPath)
	if err != nil {
		return ""
	}
	if limit > 0 && len(data) > limit {
		data = data[len(data)-limit:]
	}
	return strings.TrimSpace(string(data))
}

type commandProcess struct {
	cmd *exec.Cmd
}

func startProcess(cmd *exec.Cmd) (Process, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &commandProcess{cmd: cmd}, nil
}

func (p *commandProcess) Signal(sig os.Signal) error {
	signal, ok := sig.(syscall.Signal)
	if !ok {
		return errors.New("unsupported process signal")
	}
	return syscall.Kill(-p.cmd.Process.Pid, signal)
}

func (p *commandProcess) Wait() error {
	return p.cmd.Wait()
}

func prepareOutputDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cannot create recording directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve recording directory: %w", err)
	}
	st, err := os.Lstat(real)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("recording directory is not a real directory")
	}
	return real, nil
}

func reserveOutput(dir string, now time.Time) (string, *os.File, error) {
	stem := "screen-" + now.Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := stem + ".mp4"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.mp4", stem, i)
		}
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("cannot create recording file: %w", err)
		}
		return path, file, nil
	}
	return "", nil, errors.New("cannot allocate a unique recording filename")
}

func validMP4(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [12]byte
	n, err := file.Read(header[:])
	return err == nil && n == len(header) && string(header[4:8]) == "ftyp"
}

func freeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
