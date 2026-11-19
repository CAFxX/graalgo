// An integer that names no live object does not convert to a pointer
// (GG-0062): gc builds the pointer and faults when it is used, if ever.
package main

import "unsafe"

var addr uintptr = 0xdead0000

func main() {
	println(unsafe.Pointer(uintptr(0)) == nil)
	p := unsafe.Pointer(addr)
	println(p != nil)
}
