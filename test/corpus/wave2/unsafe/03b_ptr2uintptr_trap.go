package main

import (
	"unsafe"
)

// 03b: unsafe.Pointer → uintptr traps (D5 — the handle must never escape
// as an integer). Pre-existing trap (GoConvert.unsafePointerToUintptr).
// INTENTIONAL DIVERGENCE: gc converts freely; graalgo traps loud with
// UnsafeEscapeException (exit 1).
func main() {
	p := unsafe.Pointer(new(int))
	_ = uintptr(p)
}
