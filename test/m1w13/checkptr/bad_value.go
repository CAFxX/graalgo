// Pointer arithmetic that leaves an integer below the first legal address.
package main

import "unsafe"

var sink *int64

func main() {
	x := new(int64)
	sink = x
	u := uintptr(unsafe.Pointer(x))
	println(unsafe.Pointer(u-u) == nil)
	p := unsafe.Pointer(u - u + 8)
	println(p != nil)
}
