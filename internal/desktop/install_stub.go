//go:build !linux

package desktop

import "errors"

func listDisks() ([]diskInfo, error) {
	return nil, errors.New("此平台不支持磁盘安装")
}

func installSystem(target diskInfo, progress func(installProgress)) (installResult, error) {
	return installResult{}, errors.New("此平台不支持磁盘安装")
}
