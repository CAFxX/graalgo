// unsafe.Slice past the end of the allocation.
package main

import "unsafe"

var sink []int32

func main() {
	xs := make([]int32, 4)
	sink = xs
	println(len(unsafe.Slice(&xs[1], 3)))
	println(len(unsafe.Slice(&xs[1], 4)))
}
