package main

import (
	"fmt"
	"syscall"
)

// sc04: Getwd returns the absolute current directory.
func main() {
	wd, err := syscall.Getwd()
	fmt.Println(err == nil, len(wd) > 0, wd[0] == '/')
	// ImplementsGetwd is part of the gc contract os consults.
	fmt.Println(syscall.ImplementsGetwd)
}
