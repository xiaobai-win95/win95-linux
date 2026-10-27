//go:build linux

package gpt

import "syscall"

func syscallIoctl(fd, req, arg uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
}
