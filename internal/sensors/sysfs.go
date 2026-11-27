package sensors

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readTemperatures(sysRoot string) (cpuTemps, deviceTemps []Temperature, err error) {
	thermalRoot := filepath.Join(sysRoot, "class", "thermal")
	zones, err := sensorSubdirs(thermalRoot, "thermal_zone")
	if err != nil {
		return nil, nil, err
	}
	for _, zone := range zones {
		value, ok := readNumber(filepath.Join(zone, "temp"))
		if !ok {
			continue
		}
		value /= 1000 // Linux thermal zones expose millidegrees Celsius.
		if !validTemperature(value) {
			continue
		}
		label := readText(filepath.Join(zone, "type"))
		if label == "" {
			label = filepath.Base(zone)
		}
		temp := Temperature{
			Label:   label,
			Source:  relativeSource(sysRoot, filepath.Join(zone, "temp")),
			Celsius: value,
			Unit:    UnitCelsius,
		}
		if isCPUTemperature(label) {
			cpuTemps = append(cpuTemps, temp)
		} else {
			deviceTemps = append(deviceTemps, temp)
		}
	}

	hwmonRoot := filepath.Join(sysRoot, "class", "hwmon")
	devices, err := sensorSubdirs(hwmonRoot, "hwmon")
	if err != nil {
		return cpuTemps, deviceTemps, err
	}
	for _, device := range devices {
		chip := hwmonName(device)
		entries, err := os.ReadDir(device)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return cpuTemps, deviceTemps, wrapPathError(device, err)
		}
		for _, entry := range entries {
			if _, ok := indexedInput(entry.Name(), "temp"); !ok {
				continue
			}
			value, ok := readNumber(filepath.Join(device, entry.Name()))
			if !ok {
				continue
			}
			value /= 1000 // hwmon temperature inputs are also millidegrees Celsius.
			if !validTemperature(value) {
				continue
			}
			label := inputLabel(device, chip, entry.Name(), "temp")
			temp := Temperature{
				Label:   label,
				Source:  relativeSource(sysRoot, filepath.Join(device, entry.Name())),
				Celsius: value,
				Unit:    UnitCelsius,
			}
			if isCPUTemperature(label + " " + chip) {
				cpuTemps = append(cpuTemps, temp)
			} else {
				deviceTemps = append(deviceTemps, temp)
			}
		}
	}
	return cpuTemps, deviceTemps, nil
}

func readFans(sysRoot string) ([]Fan, error) {
	root := filepath.Join(sysRoot, "class", "hwmon")
	devices, err := sensorSubdirs(root, "hwmon")
	if err != nil {
		return nil, err
	}
	var fans []Fan
	for _, device := range devices {
		chip := hwmonName(device)
		entries, err := os.ReadDir(device)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fans, wrapPathError(device, err)
		}
		for _, entry := range entries {
			if _, ok := indexedInput(entry.Name(), "fan"); !ok {
				continue
			}
			rpm, ok := readNumber(filepath.Join(device, entry.Name()))
			if !ok || rpm < 0 {
				continue
			}
			fans = append(fans, Fan{
				Label:  inputLabel(device, chip, entry.Name(), "fan"),
				Source: relativeSource(sysRoot, filepath.Join(device, entry.Name())),
				RPM:    rpm,
				Unit:   UnitRPM,
			})
		}
	}
	return fans, nil
}

// sensorSubdirs follows class-device symlinks, which are normal in sysfs.
func sensorSubdirs(root, prefix string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, wrapPathError(root, err)
	}
	var dirs []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			// Broken links and devices that disappear during a scan are
			// indistinguishable from unsupported sensors to the caller.
			continue
		}
		dirs = append(dirs, path)
	}
	return dirs, nil
}

func hwmonName(device string) string {
	if name := readText(filepath.Join(device, "name")); name != "" {
		return name
	}
	if name := readText(filepath.Join(device, "device", "name")); name != "" {
		return name
	}
	return filepath.Base(device)
}

func inputLabel(device, chip, input, prefix string) string {
	index, _ := indexedInput(input, prefix)
	if label := readText(filepath.Join(device, fmt.Sprintf("%s%d_label", prefix, index))); label != "" {
		return label
	}
	label := strings.TrimSuffix(input, "_input")
	if chip != "" && chip != filepath.Base(device) {
		label = chip + " " + label
	}
	return label
}

func indexedInput(name, prefix string) (int, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, "_input") {
		return 0, false
	}
	indexText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), "_input")
	if indexText == "" {
		return 0, false
	}
	index, err := strconv.Atoi(indexText)
	return index, err == nil
}

func isCPUTemperature(label string) bool {
	label = strings.ToLower(label)
	for _, marker := range []string{"cpu", "core", "package", "processor", "k10temp", "zenpower", "soc", "acpitz"} {
		if strings.Contains(label, marker) {
			return true
		}
	}
	return false
}

func validTemperature(celsius float64) bool {
	return !math.IsNaN(celsius) && !math.IsInf(celsius, 0) && celsius > -273.15
}

func readText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readNumber(path string) (float64, bool) {
	text := readText(path)
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func relativeSource(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}
