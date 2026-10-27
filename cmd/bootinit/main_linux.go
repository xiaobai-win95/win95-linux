//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"win95linux/internal/gpt"
)

var (
	kmsg    *os.File
	console *os.File
)

func consolef(format string, args ...any) {
	if console == nil {
		console, _ = os.OpenFile("/dev/tty0", os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	}
	if console != nil {
		fmt.Fprintf(console, format+"\n", args...)
	}
}

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if kmsg == nil {
		kmsg, _ = os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	}
	if kmsg != nil {
		fmt.Fprintf(kmsg, "<6>win95-boot: %s\n", msg)
	}
	consolef("win95-boot: %s", msg)
}

// fatal keeps the machine alive but leaves the reason on screen: the kernel
// cmdline is quiet, so kmsg alone would present a black console. The text
// console has no CJK font, so these messages stay ASCII.
func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logf("FATAL: %s", msg)
	consolef("\nWindows 95 Linux boot failed:\n%s\n\nSystem halted. Check the VM disk settings and reboot.", msg)
	for {
		time.Sleep(time.Hour)
	}
}

func mountFS(source, target, fstype string) {
	if err := os.MkdirAll(target, 0o755); err != nil {
		logf("mkdir %s: %v", target, err)
	}
	if err := syscall.Mount(source, target, fstype, 0, ""); err != nil && err != syscall.EBUSY {
		logf("mount %s on %s: %v", fstype, target, err)
	}
}

// finitModule hands an already-open module file to the kernel. Using the
// syscall directly keeps the installed initramfs free of a userland loader.
func finitModule(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	params := []byte{0}
	_, _, errno := syscall.Syscall(syscall.SYS_FINIT_MODULE, f.Fd(), uintptr(unsafe.Pointer(&params[0])), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func loadModules() {
	data, err := os.ReadFile("/modules.load")
	if err != nil {
		logf("no module list: %v", err)
		return
	}
	failures := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := finitModule(line); err != nil {
			failures++
			if failures <= 12 {
				logf("load %s failed: %v", line, err)
			}
		}
	}
	logf("loaded modules, %d failures", failures)
}

func cmdlineValue(key string) string {
	b, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, key+"=") {
			return strings.TrimPrefix(f, key+"=")
		}
	}
	return ""
}

func findRoot() (string, error) {
	spec := cmdlineValue("win95.root")
	if spec == "" {
		spec = cmdlineValue("root")
	}
	if spec == "" {
		return "", fmt.Errorf("kernel cmdline has no win95.root")
	}
	if strings.HasPrefix(spec, "/dev/") {
		return spec, nil
	}
	uuid := spec
	if strings.HasPrefix(strings.ToUpper(uuid), "PARTUUID=") {
		uuid = uuid[len("PARTUUID="):]
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		dev, err := gpt.FindPartitionDeviceByUUID(uuid)
		if err == nil {
			if _, statErr := os.Stat(dev); statErr == nil {
				return dev, nil
			} else {
				err = statErr
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("cannot find root partition PARTUUID=%s: %v", uuid, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func switchRoot(dev string) error {
	if err := os.MkdirAll("/newroot", 0o755); err != nil {
		return err
	}
	if err := syscall.Mount(dev, "/newroot", "ext4", 0, ""); err != nil {
		return fmt.Errorf("mount root %s failed: %w", dev, err)
	}
	for _, name := range []string{"dev", "proc", "sys"} {
		if err := os.MkdirAll("/newroot/"+name, 0o755); err != nil {
			logf("mkdir /newroot/%s: %v", name, err)
			continue
		}
		if err := syscall.Mount("/"+name, "/newroot/"+name, "", syscall.MS_MOVE, ""); err != nil {
			logf("move /%s: %v", name, err)
		}
	}
	if err := syscall.Chroot("/newroot"); err != nil {
		return fmt.Errorf("chroot /newroot failed: %w", err)
	}
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / failed: %w", err)
	}
	// The check has to run after chroot: Alpine stores /sbin/init as a symlink
	// to the absolute path /bin/busybox, which only resolves inside the new root.
	if _, err := os.Stat("/sbin/init"); err != nil {
		return fmt.Errorf("root filesystem has no /sbin/init: %w", err)
	}
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"TERM=linux",
		"LANG=C.UTF-8",
	}
	return syscall.Exec("/sbin/init", []string{"/sbin/init"}, env)
}

func main() {
	_ = syscall.Sethostname([]byte("win95boot"))
	mountFS("devtmpfs", "/dev", "devtmpfs")
	mountFS("proc", "/proc", "proc")
	mountFS("sysfs", "/sys", "sysfs")
	logf("installed-system init starting")

	loadModules()
	root, err := findRoot()
	if err != nil {
		fatal("%v", err)
	}
	logf("booting installed root %s", root)
	if err := switchRoot(root); err != nil {
		fatal("%v", err)
	}
}
