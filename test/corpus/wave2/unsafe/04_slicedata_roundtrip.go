package main

import (
	"fmt"
	"unsafe"
)

// 04: SliceData → Slice round-trip. Writes through the rebuilt slice must
// be visible via the original (aliasing); SliceData of a nil slice is the
// nil pointer; SliceData is stable across calls (== parity).
func main() {
	s := []byte{1, 2, 3, 4}
	p := unsafe.SliceData(s)
	fmt.Println(p == nil) // false
	fmt.Println(unsafe.SliceData(s) == p) // true — interning

	s2 := unsafe.Slice((*byte)(p), 4)
	fmt.Println(len(s2), cap(s2)) // 4 4
	s2[0] = 9
	s2[3] = 7
	fmt.Println(s[0], s[1], s[2], s[3]) // 9 2 3 7 — aliasing

	// Sub-slice views share the backing array but differ as pointers.
	sub := s[1:3]
	fmt.Println(unsafe.SliceData(sub) == p) // false — offset rides in the key
	fmt.Println(unsafe.SliceData(sub) == unsafe.SliceData(s[1:3])) // true

	// Nil slice → nil pointer; nil pointer + 0 len → nil slice.
	var nilb []byte
	fmt.Println(unsafe.SliceData(nilb) == nil) // true
	fmt.Println(unsafe.Slice((*byte)(unsafe.SliceData(nilb)), 0) == nil) // true
}
