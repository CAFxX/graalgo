package main

import (
	"fmt"
	"unsafe"
)

// 09: address-of patterns (&s.field, &arr[i]) composed with Slice/String.
// Explicit & on variables/fields/elements is WS D in frontend/v1 (only
// &T{...} and new(T) produce pointers), so this runs under gc only.
type S struct {
	b [4]byte
}

func main() {
	s := S{b: [4]byte{1, 2, 3, 4}}
	bp := unsafe.Slice((*byte)(unsafe.Pointer(&s.b[0])), 4)
	fmt.Println(bp)
	arr := [3]int{7, 8, 9}
	ip := unsafe.Slice((*int)(unsafe.Pointer(&arr[0])), 3)
	fmt.Println(ip)
}
