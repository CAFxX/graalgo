// A misaligned pointer to a type that contains pointers.
package main

import "unsafe"

type node struct {
	next *node
	pad  [16]byte
}

var sink *node

func main() {
	n := new(node)
	sink = n
	println(*(*uint32)(unsafe.Add(unsafe.Pointer(n), 9)))
	q := (**node)(unsafe.Add(unsafe.Pointer(n), 9))
	println(q != nil)
}
