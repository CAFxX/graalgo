// The pointer one past the end of an object is a pointer, but it addresses
// nothing: a load through it is a fatal error (GG-0062). gc reads whatever
// follows the object.
package main

import "unsafe"

func main() {
	b := make([]byte, 32)
	b[31] = 9
	end := unsafe.Add(unsafe.Pointer(&b[0]), 32)
	println(*(*byte)(unsafe.Add(end, -1)))
	println(*(*byte)(end) >= 0)
}
