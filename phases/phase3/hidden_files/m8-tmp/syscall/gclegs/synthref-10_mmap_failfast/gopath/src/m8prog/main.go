package main

import _ "graalgo.os"

import "m8synth/syscall"

// sc10: Mmap fail-fast — no graalgo intrinsic in M8; re-test on the
// first real consumer.
func main() {
	d, err := syscall.Mmap(0, 0, 4096, syscall.PROT_READ, syscall.MAP_PRIVATE)
	_, _ = d, err
}
