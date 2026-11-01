package main

import (
	"unsafe"
)

// 03c: uintptr → unsafe.Pointer traps (D5 — a handle can never be
// materialized from an integer). INTENTIONAL DIVERGENCE: gc converts
// freely; graalgo raises a Go panic with the documented message
// (review finding R5: previously this surfaced as a scheduler
// "fatal internal error" crash, exit 1; now it is an uncaught Go panic,
// exit 2, recoverable by a deferred recover — see
// test/regression/unsafe-trap-panic/).
func main() {
	var u uintptr = 0x1000
	_ = unsafe.Pointer(u)
}
