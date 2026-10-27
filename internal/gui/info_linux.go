//go:build linux

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func slurp(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func memTotalMB() string {
	for _, line := range strings.Split(slurp("/proc/meminfo"), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			break
		}
		kb, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			break
		}
		return fmt.Sprintf("%d MB", (kb+1023)/1024)
	}
	return "未知"
}

func kernelName() string {
	f := strings.Fields(slurp("/proc/version"))
	if len(f) >= 3 && f[0] == "Linux" {
		return "Linux " + f[2] + " aarch64"
	}
	return "Linux aarch64"
}

func firmwareName() string {
	if _, err := os.Stat("/sys/firmware/efi"); err != nil {
		return "传统 BIOS（不支持）"
	}
	size := "64"
	if b, err := os.ReadFile("/sys/firmware/efi/fw_platform_size"); err == nil {
		size = strings.TrimSpace(string(b))
	}
	return fmt.Sprintf("UEFI %s 位 · systemd-boot", size)
}

func diskSummary() string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "未检测到磁盘"
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "dm-") ||
			strings.HasPrefix(name, "sr") || strings.HasPrefix(name, "md") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		sectors, err := strconv.ParseInt(slurp("/sys/block/"+name+"/size"), 10, 64)
		if err != nil || sectors == 0 {
			continue
		}
		mb := sectors * 512 / (1000 * 1000)
		kids, _ := filepath.Glob("/sys/block/" + name + "/" + name + "*")
		nparts := 0
		for _, k := range kids {
			if filepath.Base(k) != name {
				nparts++
			}
		}
		desc := fmt.Sprintf("/dev/%s · %d MB", name, mb)
		if nparts > 0 {
			desc += fmt.Sprintf(" · %d 个分区", nparts)
		}
		parts = append(parts, desc)
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "未检测到磁盘"
	}
	return strings.Join(parts, "；")
}

func networkSummary() string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return "未检测到网络设备"
	}
	var items []string
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		link := "已检测"
		switch slurp("/sys/class/net/" + name + "/operstate") {
		case "up":
			link = "已连接"
		case "down":
			link = "未连接"
		}
		mac := slurp("/sys/class/net/" + name + "/address")
		if mac != "" {
			items = append(items, fmt.Sprintf("%s · %s · %s", name, link, mac))
		} else {
			items = append(items, fmt.Sprintf("%s · %s", name, link))
		}
		if len(items) >= 2 {
			break
		}
	}
	if len(items) == 0 {
		return "未检测到网络设备"
	}
	return strings.Join(items, "；")
}

func inputSummary(devs []*inputDevice) (keyboard, mouse string) {
	var kbNames, ptrNames []string
	for _, d := range devs {
		name := d.name
		if name == "" {
			name = "标准输入设备"
		}
		if d.keyboard {
			kbNames = append(kbNames, name)
		}
		if d.pointer {
			ptrNames = append(ptrNames, name)
		}
	}
	if len(kbNames) == 0 {
		keyboard = "键盘: 未检测到"
	} else {
		keyboard = "键盘: " + strings.Join(kbNames, "、")
	}
	if len(ptrNames) == 0 {
		mouse = "鼠标: 未检测到"
	} else if len(ptrNames) > 1 {
		mouse = fmt.Sprintf("鼠标: %s 等 %d 个设备", ptrNames[0], len(ptrNames))
	} else {
		mouse = "鼠标: " + ptrNames[0]
	}
	return
}

func buildInfo(fb *fbDevice, devs []*inputDevice) string {
	kb, mouse := inputSummary(devs)
	return strings.Join([]string{
		fmt.Sprintf("显示: %d × %d，%d 位色", fb.w, fb.h, fb.bpp),
		fmt.Sprintf("设备: %s，行宽 %d 字节", fb.path, fb.lineBytes),
		kb,
		mouse,
		"磁盘: " + diskSummary(),
		"内存: " + memTotalMB(),
		"网络: " + networkSummary(),
		"固件: " + firmwareName(),
		"内核: " + kernelName(),
	}, "\n")
}
