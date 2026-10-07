package main

import (
	"unsafe"
)

// 03a: unsafe.Add always traps (D5/GG-0015 — handles admit no byte
// arithmetic). INTENTIONAL DIVERGENCE: gc implements Add; graalgo traps
// loud with UnsafeEscapeException (exit 1). The .panic file pins the
// exact first stderr line.
func main() {
	p := unsafe.Pointer(new(int))
	_ = unsafe.Add(p, 1)
}
