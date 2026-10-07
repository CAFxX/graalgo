package main

import _ "graalgo.os"

import "m8synth/syscall"

// sc07: arbitrary-trap fail-fast. The message pins the trap number,
// the SYS_* name reverse lookup, and the re-test trigger.
func main() {
	r1, r2, err := syscall.Syscall(syscall.SYS_GETPID, 0, 0, 0)
	_, _, _ = r1, r2, err
}
