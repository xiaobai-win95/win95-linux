//go:build !linux

package gpt

import "syscall"

func syscallIoctl(fd, req, arg uintptr) (uintptr, uintptr, syscall.Errno) {
	return 0, 0, syscall.ENOTSUP
}
