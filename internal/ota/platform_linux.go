//go:build linux

package ota

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/gpt"
)

// SuperMount is where rcS mounts the shared super partition in the installed
// system.
const SuperMount = "/mnt/win95-super"

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

// SuperUUIDFromCmdline extracts the PARTUUID from win95.super=PARTUUID=...
func SuperUUIDFromCmdline() string {
	spec := cmdlineValue("win95.super")
	if spec == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(spec), "PARTUUID=") {
		spec = spec[len("PARTUUID="):]
	}
	return strings.ToLower(strings.TrimSpace(spec))
}

func mounted(dir string) bool {
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == dir {
			return true
		}
	}
	return false
}

// EnsureSuperMounted returns the super mount point, mounting it first if rcS
// has not done so (for example when the GUI is started manually).
func EnsureSuperMounted() (string, error) {
	if mounted(SuperMount) {
		return SuperMount, nil
	}
	dev := ""
	if _, err := os.Stat("/dev/win95-super"); err == nil {
		dev = "/dev/win95-super"
	} else if uuid := SuperUUIDFromCmdline(); uuid != "" {
		if d, err := gpt.FindPartitionDeviceByUUID(uuid); err == nil {
			dev = d
		}
	}
	if dev == "" {
		return "", errors.New("找不到超级分区 /dev/win95-super")
	}
	if err := os.MkdirAll(SuperMount, 0o755); err != nil {
		return "", err
	}
	if err := syscall.Mount(dev, SuperMount, "ext4", 0, ""); err != nil && err != syscall.EBUSY {
		return "", err
	}
	if !mounted(SuperMount) {
		return "", errors.New("挂载超级分区失败")
	}
	return SuperMount, nil
}

// ConfirmRunningBoot commits a trial boot once the desktop has started
// successfully. It is a no-op for normal boots and for rollbacks.
func ConfirmRunningBoot() (bool, error) {
	slot := NormalizeSlot(os.Getenv("WIN95_SLOT"))
	if slot == "" {
		return false, nil
	}
	superDir, err := EnsureSuperMounted()
	if err != nil {
		return false, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := ReadState(superDir)
		if err == nil {
			if st.Trial != slot || NormalizeSlot(st.Booted) != slot {
				return false, nil
			}
			next, ok := ConfirmBoot(st)
			if !ok {
				return false, nil
			}
			if err := WriteState(superDir, next); err != nil {
				return false, err
			}
			syscall.Sync()
			return true, nil
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			return false, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}
