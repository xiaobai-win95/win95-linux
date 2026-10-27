package desktop

import (
	"os"
	"strings"
)

// clipboardPath is shared with the XFCE side through win95-clipd, which polls
// xclip and this file. It survives switching VTs, unlike an X selection.
const clipboardPath = "/run/win95-clipboard"

func clipboardRead() string {
	b, err := os.ReadFile(clipboardPath)
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func clipboardWrite(s string) {
	if s == "" {
		return
	}
	if err := os.MkdirAll("/run", 0o755); err != nil {
		return
	}
	tmp := clipboardPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, clipboardPath); err != nil {
		os.Remove(tmp)
	}
}
