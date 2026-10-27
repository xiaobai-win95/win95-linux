//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"win95linux/internal/desktop"
	"win95linux/internal/gui"
)

var (
	kmsgFile *os.File
	kmsgMu   sync.Mutex
)

func klog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	kmsgMu.Lock()
	defer kmsgMu.Unlock()
	if kmsgFile == nil {
		kmsgFile, _ = os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	}
	if kmsgFile != nil {
		fmt.Fprintf(kmsgFile, "<6>win95-installer: %s\n", msg)
	}
}

func mountFS(source, target, fstype, data string) {
	if err := os.MkdirAll(target, 0o755); err != nil {
		klog("mkdir %s: %v", target, err)
	}
	err := syscall.Mount(source, target, fstype, 0, data)
	if err != nil && !errors.Is(err, syscall.EBUSY) {
		klog("mount %s on %s: %v", fstype, target, err)
	}
}

func loadModules() {
	data, err := os.ReadFile("/modules.load")
	if err != nil {
		klog("no module list: %v", err)
		return
	}
	failures := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out, err := exec.Command("/bin/busybox", "insmod", line).CombinedOutput()
		if err != nil {
			failures++
			if failures <= 12 {
				klog("insmod %s failed: %v %s", filepath.Base(line), err, strings.TrimSpace(string(out)))
			}
		}
	}
	klog("module load complete, %d failures", failures)
}

func fatal(msg string) {
	klog("FATAL: %s", msg)
	if f, err := os.OpenFile("/dev/tty0", os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		fmt.Fprintf(f, "\n\nWindows 95 Linux installer could not start:\n%s\n", msg)
		f.Close()
	}
	for {
		time.Sleep(time.Hour)
	}
}

func main() {
	_ = syscall.Sethostname([]byte("win95-installer"))
	mountFS("devtmpfs", "/dev", "devtmpfs", "")
	mountFS("proc", "/proc", "proc", "")
	mountFS("sysfs", "/sys", "sysfs", "")
	mountFS("tmpfs", "/tmp", "tmpfs", "mode=1777")
	mountFS("devpts", "/dev/pts", "devpts", "gid=5,mode=0620")
	klog("installer starting as pid %d", os.Getpid())

	loadModules()
	time.Sleep(900 * time.Millisecond)

	if err := gui.Run(gui.Options{Mode: desktop.ModeInstaller}); err != nil {
		fatal(err.Error())
	}
}
