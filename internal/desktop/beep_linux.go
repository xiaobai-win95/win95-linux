//go:build linux

package desktop

import (
	"os"
	"syscall"
)

// beep rings the PC speaker through the console bell. UTM may not expose a
// PC speaker, so failures are ignored.
func beep() {
	for _, path := range []string{"/dev/tty0", "/dev/console"} {
		f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
		if err != nil {
			continue
		}
		_, _ = f.WriteString("\a")
		_ = f.Close()
		return
	}
}

// BootBeep plays the classic single POST beep when the desktop starts.
func BootBeep() { beep() }
