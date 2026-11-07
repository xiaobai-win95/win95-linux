//go:build !linux

package desktop

// bwSound is a no-op off Linux so native tests and preview builds stay silent.
func bwSound(kind string) {}
