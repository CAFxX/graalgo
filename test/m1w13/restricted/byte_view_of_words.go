// A []byte over the bytes of wider scalars cannot share storage on the flat
// value model (one slice shape: a []byte addresses byte leaves; ADR-0015
// decision 3). gc accepts this program even with -d=checkptr; graalgo stops
// with a checkptr fatal error instead of running it wrongly (SPEC §3.4: "the
// restriction relative to gc without checkptr is documented" —
// docs/arch/limitations.md). Scalar loads and stores through the same
// pointer work.
package main

import "unsafe"

func main() {
	ints := []int32{0x04030201, 0x08070605}
	p := unsafe.Pointer(&ints[0])
	println(*(*byte)(p), *(*byte)(unsafe.Add(p, 5)))
	wide := unsafe.Slice((*byte)(p), 8)
	println(len(wide))
}
