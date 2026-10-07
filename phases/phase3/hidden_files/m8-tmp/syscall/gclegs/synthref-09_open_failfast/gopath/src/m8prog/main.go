package main

import _ "graalgo.os"

import "m8synth/syscall"

// sc09: I/O wrapper fail-fast — syscall.Open has no graalgo intrinsic
// in M8 (single GoOs handle domain, owned by os); the message names
// the fd-domain unification re-test trigger.
func main() {
	fd, err := syscall.Open("/nonexistent-dir-xyz/f", syscall.O_RDONLY, 0)
	_, _ = fd, err
}
