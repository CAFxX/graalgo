package main

import (
	"unsafe"
)

// 03a: unsafe.Add always traps (D5/GG-0015 — handles admit no byte
// arithmetic). INTENTIONAL DIVERGENCE: gc implements Add; graalgo traps
// with a Go panic carrying the documented message (exit 2), never a
// host-side internal-error crash. The .panic file pins the exact first
// stderr line.
func main() {
	p := unsafe.Pointer(new(int))
	_ = unsafe.Add(p, 1)
}
