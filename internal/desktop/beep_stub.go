//go:build !linux

package desktop

func beep() {}

// BootBeep exists so preview builds can share the Linux startup path.
func BootBeep() {}
