package main

import (
	"unsafe"
)

// 03c: uintptr → unsafe.Pointer traps (D5 — a handle can never be
// materialized from an integer). INTENTIONAL DIVERGENCE: gc converts
// freely; graalgo traps with a Go panic carrying the documented message
// (exit 2), never a host-side internal-error crash.
func main() {
	var u uintptr = 0x1000
	_ = unsafe.Pointer(u)
}
