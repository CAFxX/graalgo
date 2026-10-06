package main

import (
	"fmt"
	_ "unsafe"
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

func main() {
	// Range: 1000 samples of fastrandn(100) are all in [0,100).
	bad := 0
	for i := 0; i < 1000; i++ {
		v := fastrandn(100)
		if v >= 100 {
			bad++
		}
	}
	fmt.Println("range bad:", bad)

	// fastrandn(1) is always 0.
	zero := true
	for i := 0; i < 100; i++ {
		if fastrandn(1) != 0 {
			zero = false
		}
	}
	fmt.Println("n=1 all zero:", zero)
}
