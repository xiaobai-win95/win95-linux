package gui

import (
	"errors"

	"win95linux/internal/desktop"
)

// ErrLogout tells the session supervisor that the user logged off, as opposed
// to the GUI crashing. win95gui exits with status 42 for a logoff.
var ErrLogout = errors.New("user logged off")

// Options selects which shell the runtime should present.
type Options struct {
	Mode desktop.Mode
	// Info, when non-empty, replaces the automatically detected hardware text
	// shown by the About dialog.
	Info string
}
