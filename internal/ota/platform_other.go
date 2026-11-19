//go:build !linux

package ota

import "errors"

const SuperMount = "/mnt/win95-super"

func SuperUUIDFromCmdline() string { return "" }

func EnsureSuperMounted() (string, error) {
	return "", errors.New("OTA 更新只能在目标系统内执行")
}

func ConfirmRunningBoot() (bool, error) { return false, nil }
