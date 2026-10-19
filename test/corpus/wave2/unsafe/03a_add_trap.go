package main

import (
	"unsafe"
)

// 03a: unsafe.Add always traps (D5/GG-0015 — handles admit no byte
// arithmetic). INTENTIONAL DIVERGENCE: gc implements Add; graalgo raises a
// Go panic carrying the documented trap message (recoverable via Go
// recover() — see the UnsafeAddTrapTest end-to-end test) instead of an
// internal-error crash (B-6 interim, RV22-P1-8). The .panic file pins the
// exact first stderr line.
func main() {
	p := unsafe.Pointer(new(int))
	_ = unsafe.Add(p, 1)
}
