//go:build !linux

package gui

import "errors"

// Run is only available on Linux ARM64.
func Run(Options) error {
	return errors.New("图形界面仅支持 Linux ARM64")
}
