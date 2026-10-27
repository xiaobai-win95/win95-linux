//go:build linux

package gui

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"win95linux/internal/desktop"
	"win95linux/internal/ui"
)

const pidPath = "/run/win95gui.pid"

var (
	logMu     sync.Mutex
	kmsg      *os.File
	kmsgTried bool
	// consoleActive is false while another VT owns the display; writing to
	// /dev/tty0 then would scribble over its text console.
	consoleActive atomic.Bool
)

// consolef writes straight to the active VT. The installed system starts the
// GUI from a shell script that redirects stderr to a log, so without this a
// startup failure would leave the user on a silent black console.
func consolef(format string, args ...any) {
	tty, err := os.OpenFile("/dev/tty0", os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return
	}
	fmt.Fprintf(tty, format, args...)
	tty.Close()
}

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logMu.Lock()
	defer logMu.Unlock()
	if !kmsgTried {
		kmsgTried = true
		kmsg, _ = os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	}
	if kmsg != nil {
		fmt.Fprintf(kmsg, "<6>win95gui: %s\n", msg)
	}
	fmt.Fprintf(os.Stderr, "win95gui: %s\n", msg)
	if consoleActive.Load() {
		consolef("win95gui: %s\n", msg)
	}
}

func rebootNow(cmd int) {
	syscall.Sync()
	if err := syscall.Reboot(cmd); err != nil {
		logf("reboot failed: %v", err)
	}
}

// systemAction asks BusyBox init to power off or reboot. win95gui is not PID 1
// in the installed system, so syscall.Reboot would return EPERM there; the
// installer still runs as PID 1 and uses the syscall directly.
func systemAction(action string, cmd int) {
	syscall.Sync()
	if os.Getpid() != 1 {
		for _, path := range []string{"/sbin/" + action, "/bin/" + action} {
			if _, err := os.Stat(path); err != nil {
				continue
			}
			c := exec.Command(path)
			c.Dir = "/"
			c.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root"}
			if err := c.Run(); err == nil {
				logf("%s requested", action)
				return
			} else {
				logf("%s failed: %v", action, err)
			}
		}
	}
	rebootNow(cmd)
}

// suspendSystem writes a sleep state to sysfs. The write blocks until the
// machine wakes; suspend-to-idle (freeze) is the safest choice in a VM, so it
// is preferred over deep memory sleep.
func suspendSystem() error {
	b, err := os.ReadFile("/sys/power/state")
	if err != nil {
		return fmt.Errorf("读取 /sys/power/state 失败: %w", err)
	}
	states := strings.Fields(string(b))
	has := func(s string) bool {
		for _, v := range states {
			if v == s {
				return true
			}
		}
		return false
	}
	var candidates []string
	for _, s := range []string{"freeze", "mem"} {
		if has(s) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return fmt.Errorf("内核不支持睡眠（可用状态：%s）", strings.Join(states, " "))
	}
	var lastErr error
	for _, target := range candidates {
		f, err := os.OpenFile("/sys/power/state", os.O_WRONLY, 0)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = f.WriteString(target)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("写入睡眠状态失败: %w", lastErr)
}

func writePIDFile() (func(), bool) {
	pid := strconv.Itoa(os.Getpid())
	if err := os.MkdirAll("/run", 0o755); err != nil {
		return nil, false
	}
	if err := os.WriteFile(pidPath, []byte(pid+"\n"), 0o644); err != nil {
		return nil, false
	}
	return func() {
		if b, err := os.ReadFile(pidPath); err == nil && strings.TrimSpace(string(b)) == pid {
			_ = os.Remove(pidPath)
		}
	}, true
}

func reopenFramebuffer(path string) (*fbDevice, error) {
	var lastErr error
	for i := 0; i < 20; i++ {
		f, err := openFramebuffer(path)
		if err == nil {
			return f, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, lastErr
}

func drainInput(ch <-chan inputEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// Run takes over the framebuffer and runs the desktop event loop until the
// process is replaced or killed.
func Run(opt Options) error {
	consoleActive.Store(true)
	sigs := make(chan os.Signal, 16)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)

	fb, err := findFramebuffer()
	if err != nil {
		consolef("Windows 95 Linux: display init failed: %v\n", err)
		return fmt.Errorf("显示初始化失败: %v", err)
	}
	fbPath := fb.path
	defer func() {
		if fb != nil {
			fb.close()
		}
	}()
	logf("framebuffer %s %dx%d %dbpp line=%d direct=%v", fb.path, fb.w, fb.h, fb.bpp, fb.lineBytes, fb.direct)

	devs := waitForInputs()
	for _, d := range devs {
		logf("input %s name=%q keyboard=%v pointer=%v abs=%dx%d", d.path, d.name, d.keyboard, d.pointer, d.absMaxX, d.absMaxY)
	}
	if len(devs) == 0 {
		logf("warning: no input devices found")
	}

	// A VT in KD_GRAPHICS with VT_AUTO silently drops console switches, which
	// leaves chvt blocked in VT_WAITACTIVE. VT_PROCESS makes the kernel send
	// SIGUSR1/SIGUSR2 so we can release and reacquire the framebuffer.
	vt, err := openVTController()
	if err != nil {
		logf("VT control unavailable: %v (console switching stays in VT_AUTO)", err)
		vt = nil
	}
	defer vt.restore()
	defer setConsoleText(vt)
	setConsoleQuiet(vt)

	cleanupPID, havePID := writePIDFile()
	if havePID {
		defer cleanupPID()
	}

	info := opt.Info
	if info == "" {
		info = buildInfo(fb, devs)
	}

	wake := make(chan struct{}, 1)
	quit := make(chan struct{}, 1)
	host := desktop.Host{
		Info: info,
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
		Reboot: func() {
			systemAction("reboot", syscall.LINUX_REBOOT_CMD_RESTART)
		},
		PowerOff: func() {
			systemAction("poweroff", syscall.LINUX_REBOOT_CMD_POWER_OFF)
		},
		Suspend: suspendSystem,
		Logout: func() {
			select {
			case quit <- struct{}{}:
			default:
			}
		},
	}
	d := desktop.New(fb.w, fb.h, opt.Mode, host)

	events := make(chan inputEvent, 2048)
	for _, dev := range devs {
		go dev.readLoop(events)
	}

	canvas := ui.NewCanvas(fb.w, fb.h)
	state := newInputState(fb.w, fb.h)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	// Short frame ticker drives the screensaver animation. The desktop only
	// invalidates the screen when something is actually moving.
	frame := time.NewTicker(40 * time.Millisecond)
	defer frame.Stop()

	foreground := true // our VT is the active one
	if myVT := vt.number(); myVT > 0 {
		if fg, ok := vt.foreground(); ok && fg != myVT {
			foreground = false
			consoleActive.Store(false)
			fb.close()
			fb = nil
			logf("VT%d is not foreground (active VT%d); waiting for acquire", myVT, fg)
		}
	}
	lastDraw := time.Time{}
	for {
		d.RunPosted()
		d.Tick(time.Now())
		if foreground && fb != nil && d.NeedsRedraw() {
			if wait := 16*time.Millisecond - time.Since(lastDraw); wait > 0 {
				time.Sleep(wait)
			}
			d.RunPosted()
			d.Draw(canvas)
			fb.blit(canvas)
			lastDraw = time.Now()
		}
		select {
		case ev := <-events:
			if !foreground || fb == nil {
				drainInput(events)
				continue
			}
			state.handle(ev, d)
			d.Invalidate()
			for n := 0; n < 64; n++ {
				select {
				case ev = <-events:
					state.handle(ev, d)
					d.Invalidate()
				default:
					n = 64
				}
			}
		case sig := <-sigs:
			switch sig {
			case syscall.SIGUSR1:
				consoleActive.Store(false)
				if foreground {
					foreground = false
					if fb != nil {
						fb.close()
						fb = nil
					}
					drainInput(events)
				}
				vt.ackRelease()
				logf("VT released: framebuffer closed")
			case syscall.SIGUSR2:
				// A quick switch back and away can leave this acquire queued
				// after the kernel already moved to another VT. Only take the
				// framebuffer back if tty1 is really in the foreground.
				if myVT := vt.number(); myVT > 0 {
					if fg, ok := vt.foreground(); ok && fg != myVT {
						consoleActive.Store(false)
						foreground = false
						if fb != nil {
							fb.close()
							fb = nil
						}
						drainInput(events)
						vt.ackAcquire()
						logf("stale acquire ignored (active VT%d)", fg)
						continue
					}
				}
				consoleActive.Store(true)
				if foreground && fb != nil {
					vt.ackAcquire()
					continue
				}
				foreground = true
				nfb, err := reopenFramebuffer(fbPath)
				if err != nil {
					// Keep acking so the console is not frozen; the tick
					// handler below retries the device.
					vt.ackAcquire()
					logf("VT acquired but framebuffer reopen failed: %v", err)
					continue
				}
				fb = nfb
				vt.setGraphics(true)
				state.resetModifiers()
				drainInput(events)
				if canvas.W == fb.w && canvas.H == fb.h {
					fb.blit(canvas)
				}
				d.Invalidate()
				lastDraw = time.Now()
				vt.ackAcquire()
				logf("VT acquired: framebuffer reopened %dx%d", fb.w, fb.h)
			default:
				logf("received %v, shutting down", sig)
				return nil
			}
		case <-wake:
		case <-quit:
			logf("user logged off")
			return ErrLogout
		case <-frame.C:
			d.Animate(time.Now())
		case <-tick.C:
			if foreground && fb == nil {
				if nfb, err := openFramebuffer(fbPath); err == nil {
					fb = nfb
					vt.setGraphics(true)
					state.resetModifiers()
					drainInput(events)
					d.Invalidate()
					logf("framebuffer reopened after retry: %dx%d", fb.w, fb.h)
				}
			}
		}
	}
}
