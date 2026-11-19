// unsafe.String past the end of the allocation.
package main

import "unsafe"

var sink []byte

func main() {
	b := []byte("0123456789abcdef")
	sink = b
	println(unsafe.String(&b[10], 6))
	println(unsafe.String(&b[10], 7))
}
