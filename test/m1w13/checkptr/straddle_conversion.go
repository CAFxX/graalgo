// (*[5]int64)(unsafe.Pointer(x)) with x a [4]int64: the converted pointer's
// pointee reaches past the object. gc with -d=checkptr and graalgo always
// (SPEC §3.4, GG-0062) stop with the same fatal error; no deferred call runs
// and recover does not see it.
package main

import "unsafe"

var sink *[4]int64

func main() {
	defer println("deferred calls do not run after a fatal error")
	defer func() { recover() }()
	x := new([4]int64)
	sink = x
	println((*[4]int64)(unsafe.Pointer(x))[3], (*[2]int64)(unsafe.Pointer(&x[2]))[1])
	p := (*[5]int64)(unsafe.Pointer(x))
	println(p[0])
}
