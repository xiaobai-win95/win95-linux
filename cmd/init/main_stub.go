//go:build !linux

package main

import "fmt"

func main() {
	fmt.Println("cmd/init only builds for linux/arm64 (use build-smoke.sh)")
}
