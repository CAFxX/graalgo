package main

import (
	"unsafe"
)

// 03a: unsafe.Add always traps (v1 D5 — handles admit no byte
// arithmetic; GG-0062 defines in-object Add and supersedes this). INTENTIONAL DIVERGENCE: gc implements Add; graalgo traps
// loud with UnsafeEscapeException (exit 1). The .panic file pins the
// exact first stderr line.
func main() {
	p := unsafe.Pointer(new(int))
	_ = unsafe.Add(p, 1)
}
