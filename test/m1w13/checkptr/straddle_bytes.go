// *(*uint64)(unsafe.Pointer(&b[60])) on a 64-byte slice: four of the eight
// bytes are outside the allocation.
package main

import "unsafe"

var sink []byte

func main() {
	b := make([]byte, 64)
	sink = b
	b[59] = 7
	println(*(*uint32)(unsafe.Pointer(&b[56])), *(*uint64)(unsafe.Pointer(&b[56])))
	println(*(*uint64)(unsafe.Pointer(&b[60])))
}
