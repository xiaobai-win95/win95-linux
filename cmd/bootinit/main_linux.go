//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"win95linux/internal/gpt"
	"win95linux/internal/ota"
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

func mountFS(source, target, fstype string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		logf("mkdir %s: %v", target, err)
		return err
	}
	if err := syscall.Mount(source, target, fstype, 0, ""); err != nil && err != syscall.EBUSY {
		logf("mount %s on %s: %v", fstype, target, err)
		return err
	}
	return nil
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

func splitPartUUID(spec string) string {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(strings.ToUpper(spec), "PARTUUID=") {
		spec = spec[len("PARTUUID="):]
	}
	return strings.ToLower(spec)
}

func findDevice(spec string) (string, error) {
	if spec == "" {
		return "", fmt.Errorf("kernel cmdline has no root device")
	}
	if strings.HasPrefix(spec, "/dev/") {
		return spec, nil
	}
	uuid := splitPartUUID(spec)
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
			return "", fmt.Errorf("cannot find partition PARTUUID=%s: %v", uuid, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func findRoot() (string, error) {
	spec := cmdlineValue("win95.root")
	if spec == "" {
		spec = cmdlineValue("root")
	}
	return findDevice(spec)
}

// switchRoot enters an already-mounted directory and execs its init.
func switchRoot(rootDir string, extraEnv ...string) error {
	for _, name := range []string{"dev", "proc", "sys"} {
		if err := os.MkdirAll(filepath.Join(rootDir, name), 0o755); err != nil {
			logf("mkdir %s/%s: %v", rootDir, name, err)
			continue
		}
		if err := syscall.Mount("/"+name, filepath.Join(rootDir, name), "", syscall.MS_MOVE, ""); err != nil {
			logf("move /%s: %v", name, err)
		}
	}
	if err := syscall.Chroot(rootDir); err != nil {
		return fmt.Errorf("chroot %s failed: %w", rootDir, err)
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
	env = append(env, extraEnv...)
	return syscall.Exec("/sbin/init", []string{"/sbin/init"}, env)
}

func slotRoot(superDir, slot string) string {
	return filepath.Join(superDir, ota.SlotsDir, slot)
}

// slotValid must not follow /sbin/init: Alpine stores it as a symlink to the
// absolute path /bin/busybox, which would resolve against the initramfs root
// before chroot and never exist there. Lstat plus an explicit busybox check
// validates the slot without leaving it.
func slotValid(superDir, slot string) bool {
	if !ota.ValidSlot(slot) {
		return false
	}
	root := slotRoot(superDir, slot)
	for _, rel := range []string{"sbin/init", "bin/busybox"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return false
		}
	}
	return true
}

func slotVersion(superDir, slot string) string {
	info := ota.ReadVersionFile(filepath.Join(slotRoot(superDir, slot), "etc", "win95-version"))
	return info.Version
}

// loadOrInitState reads slot metadata, rebuilding it if this disk was
// installed by an older image that predates the super layout.
func loadOrInitState(superDir, superUUID string) (ota.State, error) {
	st, err := ota.ReadState(superDir)
	if err == nil {
		if v := slotVersion(superDir, st.Active); v != "" && st.Slots[st.Active].Version == "" {
			slot := st.Slots[st.Active]
			slot.Version = v
			st.Slots[st.Active] = slot
		}
		return st, nil
	}
	if !os.IsNotExist(err) {
		logf("read OTA state failed: %v", err)
	}
	aOK := slotValid(superDir, ota.SlotA)
	bOK := slotValid(superDir, ota.SlotB)
	switch {
	case aOK:
		st = ota.NewState(superUUID, slotVersion(superDir, ota.SlotA))
	case bOK:
		st = ota.NewState(superUUID, slotVersion(superDir, ota.SlotB))
		st.Active = ota.SlotB
		st.Slots[ota.SlotA] = ota.SlotState{Status: "empty"}
		st.Slots[ota.SlotB] = ota.SlotState{Version: slotVersion(superDir, ota.SlotB), Status: "good"}
	default:
		return st, fmt.Errorf("super partition has no bootable slot under %s", filepath.Join(superDir, ota.SlotsDir))
	}
	if werr := ota.WriteState(superDir, st); werr != nil {
		logf("write initial OTA state failed: %v", werr)
	}
	return st, nil
}

// bootSuper picks a slot from the shared super partition and enters it.
func bootSuper(spec string) {
	superUUID := splitPartUUID(spec)
	dev, err := findDevice(spec)
	if err != nil {
		fatal("%v", err)
	}
	const superDir = "/super"
	if err := mountFS(dev, superDir, "ext4"); err != nil {
		fatal("cannot mount super partition %s: %v", dev, err)
	}
	st, err := loadOrInitState(superDir, superUUID)
	if err != nil {
		fatal("%v", err)
	}

	plan, next, changed, err := ota.PlanBoot(st, func(slot string) bool {
		return slotValid(superDir, slot)
	})
	if err != nil {
		fatal("%v", err)
	}
	if plan.Rollback != "" {
		logf("rollback: %s", plan.Note)
	}
	if plan.Slot == "" {
		fatal("no bootable slot")
	}
	next.Booted = plan.Slot
	if live := slotVersion(superDir, plan.Slot); live != "" && next.Slots[plan.Slot].Version == "" {
		slot := next.Slots[plan.Slot]
		slot.Version = live
		next.Slots[plan.Slot] = slot
		changed = true
	}
	if changed || st.Booted != plan.Slot {
		if err := ota.WriteState(superDir, next); err != nil {
			logf("write OTA state failed: %v", err)
		}
	}
	syscall.Sync()

	// rcS uses this node to mount the super partition for the update client.
	_ = os.Remove("/dev/win95-super")
	if err := os.Symlink(dev, "/dev/win95-super"); err != nil {
		logf("create /dev/win95-super: %v", err)
	}

	logf("super %s state: active=%s trial=%s attempts=%d", dev, next.Active, next.Trial, next.Attempts)
	logf("booting slot %s (%s)", plan.Slot, slotRoot(superDir, plan.Slot))
	if err := switchRoot(slotRoot(superDir, plan.Slot), "WIN95_SLOT="+plan.Slot, "WIN95_LAYOUT="+ota.Layout); err != nil {
		fatal("%v", err)
	}
}

func main() {
	_ = syscall.Sethostname([]byte("win95boot"))
	mountFS("devtmpfs", "/dev", "devtmpfs")
	mountFS("proc", "/proc", "proc")
	mountFS("sysfs", "/sys", "sysfs")
	logf("installed-system init starting")

	loadModules()
	if spec := cmdlineValue("win95.super"); spec != "" {
		bootSuper(spec)
		return
	}
	// Legacy single-partition installs keep working with the same initrd.
	dev, err := findRoot()
	if err != nil {
		fatal("%v", err)
	}
	logf("booting legacy installed root %s", dev)
	if err := os.MkdirAll("/newroot", 0o755); err != nil {
		fatal("mkdir /newroot: %v", err)
	}
	if err := syscall.Mount(dev, "/newroot", "ext4", 0, ""); err != nil {
		fatal("mount root %s failed: %v", dev, err)
	}
	if err := switchRoot("/newroot"); err != nil {
		fatal("%v", err)
	}
}
