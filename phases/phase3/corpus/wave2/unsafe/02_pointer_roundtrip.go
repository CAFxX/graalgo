package main

import (
	"fmt"
	"unsafe"
)

// 02: Pointer round-trips and interning == parity.
// p1 == p2 for two conversions of the same pointer must be true (gc);
// handle 0 is the nil pointer and compares equal to nil.
func main() {
	gp := new(int)
	p1 := unsafe.Pointer(gp)
	p2 := unsafe.Pointer(gp)
	fmt.Println(p1 == p2) // true — interning
	fmt.Println(p1 != p2) // false

	q1 := unsafe.Pointer(new(int))
	fmt.Println(p1 == q1) // false — different referents

	// *T -> Pointer -> *T round trip preserves the referent.
	back := (*int)(p1)
	fmt.Println(unsafe.Pointer(back) == p1) // true

	// Nil round-trips.
	var np *int
	nilup := unsafe.Pointer(np)
	fmt.Println(nilup == nil) // true
	fmt.Println(unsafe.Pointer(nil) == nil) // true
	var _ *int = (*int)(nilup) // nil back-conversion
	fmt.Println((*int)(nilup) == nil) // true
}
