//go:build !linux

package main

import "fmt"

func main() {
	fmt.Println("cmd/gui only builds for linux/arm64")
}
