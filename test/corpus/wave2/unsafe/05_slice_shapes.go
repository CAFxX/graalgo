package main

import (
	"fmt"
	"unsafe"
)

// 05: Slice referent shapes — whole-array backing, single T (len 1),
// explicit type arguments, and zero-length views.
func main() {
	// Whole-array backing via new([4]int).
	ap := unsafe.Pointer(new([4]int))
	a := unsafe.Slice((*int)(ap), 4)
	fmt.Println(len(a), cap(a)) // 4 4
	a[0] = 11
	a[3] = 44
	// A second Slice over the same pointer aliases the first.
	a2 := unsafe.Slice((*int)(ap), 4)
	fmt.Println(a2[0], a2[3]) // 11 44

	// Single T with len 1.
	sp := unsafe.Pointer(new(int64))
	s := unsafe.Slice((*int64)(sp), 1)
	fmt.Println(len(s)) // 1
	s[0] = -5
	sb := unsafe.Slice((*int64)(sp), 1)
	fmt.Println(sb[0]) // -5 — aliasing through the interned referent

	// Inferred element type from the pointer argument (unsafe.Slice is a
	// builtin, not a generic function — explicit type arguments like
	// unsafe.Slice[byte] are rejected, exactly as in gc).
	ep := unsafe.Pointer(new([2]byte))
	e := unsafe.Slice((*byte)(ep), 2)
	e[1] = 255
	fmt.Println(e[0], e[1]) // 0 255

	// Zero length over a non-nil pointer.
	z := unsafe.Slice((*int)(ap), 0)
	fmt.Println(len(z) == 0, z == nil) // true false
}
