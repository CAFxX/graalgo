package main

import (
	"fmt"
	"unsafe"
)

// 06: StringData → String round-trips. Content equality; empty string maps
// to the nil pointer (gc: StringData("") == nil); nil pointer + 0 len is "".
func main() {
	str := "hello, 世界"
	p := unsafe.StringData(str)
	fmt.Println(p == nil) // false
	fmt.Println(unsafe.StringData(str) == p) // true — interning

	back := unsafe.String((*byte)(p), len(str))
	fmt.Println(back == str) // true — content equality
	fmt.Println(len(back))   // 13 (bytes, not runes)

	// Empty string → nil pointer; empty String over nil pointer.
	fmt.Println(unsafe.StringData("") == nil) // true
	fmt.Println(unsafe.String(nil, 0) == "")  // true
	fmt.Println(len(unsafe.String(nil, 0)))   // 0

	// String over a []byte's data.
	b := []byte("abc")
	bs := unsafe.String((*byte)(unsafe.SliceData(b)), len(b))
	fmt.Println(bs) // abc
}
