package main

import (
	"unsafe"
)

// 03c: uintptr → unsafe.Pointer traps (D5 — a handle can never be
// materialized from an integer). INTENTIONAL DIVERGENCE: gc converts
// freely; graalgo traps loud with UnsafeEscapeException (exit 1).
func main() {
	var u uintptr = 0x1000
	_ = unsafe.Pointer(u)
}
