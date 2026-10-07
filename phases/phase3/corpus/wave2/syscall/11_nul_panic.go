// 11_nul_panic: syscall.StringByteSlice panics on NUL (P1-1).
// gc 1.27.1 panics with "syscall: string with NUL passed to
// StringByteSlice"; the synthetic must match byte-exactly.
// Uses the synthetic syscall source (has the NUL check).
package main

import (
	"syscall"
)

func main() {
	// This must panic; the .panic file pins the exact message.
	_ = syscall.StringByteSlice("a\x00b")
}
