//go:build !linux

package main

import "fmt"

func main() {
	fmt.Println("cmd/bootinit only builds for linux/arm64")
}
