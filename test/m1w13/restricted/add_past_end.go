// unsafe.Add far past the end of the object: gc's checkptr reports it only
// when the result happens to land in another heap object; graalgo always
// does — a pointer never leaves its referent (GG-0062). One past the end is
// a pointer; it cannot be dereferenced.
package main

import "unsafe"

func main() {
	xs := make([]int64, 4)
	end := unsafe.Add(unsafe.Pointer(&xs[0]), 32)
	println(uintptr(end)-uintptr(unsafe.Pointer(&xs[0])), *(*int64)(unsafe.Add(end, -8)))
	far := unsafe.Add(unsafe.Pointer(&xs[0]), 4096)
	println(far != nil)
}
