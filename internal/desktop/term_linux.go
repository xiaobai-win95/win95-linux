//go:build linux

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	ioctlTIOCSPTLCK = 0x40045431
	ioctlTIOCGPTN   = 0x80045430
	ioctlTIOCSWINSZ = 0x5414
)

type winsize struct {
	Rows uint16
	Cols uint16
	X    uint16
	Y    uint16
}

type termProc struct {
	master *os.File
	cmd    *exec.Cmd
	pid    int
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func setWinsize(fd uintptr, cols, rows int) {
	ws := winsize{Rows: uint16(rows), Cols: uint16(cols)}
	_ = ioctl(fd, ioctlTIOCSWINSZ, unsafe.Pointer(&ws))
}

func startTerminal(cols, rows int) (*termProc, error) {
	if cols < 20 {
		cols = 20
	}
	if rows < 6 {
		rows = 6
	}
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("打开 /dev/ptmx 失败: %w", err)
	}
	var unlock int32
	if err := ioctl(master.Fd(), ioctlTIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		master.Close()
		return nil, fmt.Errorf("解锁伪终端失败: %w", err)
	}
	var ptn uint32
	if err := ioctl(master.Fd(), ioctlTIOCGPTN, unsafe.Pointer(&ptn)); err != nil {
		master.Close()
		return nil, fmt.Errorf("获取伪终端编号失败: %w", err)
	}
	slavePath := "/dev/pts/" + strconv.Itoa(int(ptn))
	slave, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("打开 %s 失败: %w", slavePath, err)
	}
	setWinsize(master.Fd(), cols, rows)

	shell := "/bin/ash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-i")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.Dir = "/root"
	if _, err := os.Stat(cmd.Dir); err != nil {
		cmd.Dir = "/"
	}
	cmd.Env = []string{
		"TERM=xterm-256color",
		"HOME=/root",
		"PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin",
		"LANG=C.UTF-8",
		"ENV=/etc/profile",
		"PS1=\\033[1;32m\\u@win95\\033[0m:\\033[1;34m\\w\\033[0m\\$ ",
		"HOSTNAME=win95",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		slave.Close()
		master.Close()
		return nil, fmt.Errorf("启动 shell 失败: %w", err)
	}
	slave.Close()
	return &termProc{master: master, cmd: cmd, pid: cmd.Process.Pid}, nil
}

func (p *termProc) read(b []byte) (int, error) {
	return p.master.Read(b)
}

func (p *termProc) write(b []byte) {
	_, _ = p.master.Write(b)
}

func (p *termProc) resize(cols, rows int) {
	if p == nil || p.master == nil {
		return
	}
	setWinsize(p.master.Fd(), cols, rows)
}

func (p *termProc) close() {
	if p == nil {
		return
	}
	if p.pid > 0 {
		_ = syscall.Kill(-p.pid, syscall.SIGHUP)
		_ = syscall.Kill(-p.pid, syscall.SIGKILL)
	}
	if p.master != nil {
		p.master.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Release()
	}
}
