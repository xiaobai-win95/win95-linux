//go:build linux

package desktop

import (
	"os"
	"sync"
	"syscall"
	"time"
)

// Game sound effects use the PC speaker via the KIOCSOUND ioctl. UTM may not
// expose a speaker, so every failure is ignored and play never blocks the
// render loop (each effect runs in its own goroutine, serialized by a mutex).
const bwKIOCSOUND = 0x4B20

var bwSoundMu sync.Mutex

func bwTone(freq, ms int) {
	var f *os.File
	var err error
	for _, path := range []string{"/dev/console", "/dev/tty0"} {
		f, err = os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
		if err == nil {
			break
		}
	}
	if err != nil || f == nil {
		return
	}
	defer f.Close()
	period := uintptr(0)
	if freq > 0 {
		period = uintptr(1193180 / freq)
	}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), bwKIOCSOUND, period)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), bwKIOCSOUND, 0)
}

func bwSound(kind string) {
	go func() {
		bwSoundMu.Lock()
		defer bwSoundMu.Unlock()
		switch kind {
		case "dig":
			bwTone(180, 40)
		case "place":
			bwTone(320, 50)
		case "jump":
			bwTone(440, 55)
		case "craft":
			bwTone(523, 70)
			bwTone(659, 90)
		case "deny":
			bwTone(120, 120)
		}
	}()
}
