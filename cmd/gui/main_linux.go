//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	"win95linux/internal/desktop"
	"win95linux/internal/gui"
)

func main() {
	if err := gui.Run(gui.Options{Mode: desktop.ModeDesktop}); err != nil {
		if errors.Is(err, gui.ErrLogout) {
			os.Exit(42)
		}
		fmt.Fprintf(os.Stderr, "win95gui: %v\n", err)
		os.Exit(1)
	}
}
