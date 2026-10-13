package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// Carry-chain sweep: Add(0xFF..FF, 1, 0) at every width.
	for _, w := range []int{8, 16, 32, 64} {
		var max uint64 = 1<<w - 1
		s, c := bits.Add64(max, 1, 0)
		fmt.Println(w, s == 0, c == 1)
	}
	// Add/Sub/Mul/Div combined: (x*y)/y == x and (x*y)%y == 0 for a
	// sweep of odd y (exercises Div64's quotient-correction loops).
	for _, y := range []uint64{1, 3, 7, 0xFFFFFFFF, 0x100000001, 0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF} {
		x := uint64(0x123456789ABCDEF0)
		hi, lo := bits.Mul64(x, y)
		// Skip y where the quotient overflows (hi >= y after Mul would
		// panic in Div; choose x,y with hi < y).
		if hi >= y {
			fmt.Println("skip", y)
			continue
		}
		q, r := bits.Div64(hi, lo, y)
		fmt.Println(q == x, r == 0)
	}
	// RotateLeft/Reverse composition: Reverse(RotateLeft(x,k)) ==
	// RotateLeft(Reverse(x), -k).
	x := uint(0x0F0F0F0FF0F0F0F0)
	for _, k := range []int{0, 1, 7, 31, 63, 64, -1} {
		l := bits.RotateLeft(x, k)
		fmt.Println(bits.Reverse(l) == bits.RotateLeft(bits.Reverse(x), -k))
	}
	// Len/OnesCount/TrailingZeros consistency on a bit walk.
	for i := 0; i < 64; i++ {
		v := uint64(1) << i
		ok := bits.Len64(v) == i+1 && bits.OnesCount64(v) == 1 && bits.TrailingZeros64(v) == i
		if !ok {
			fmt.Println("mismatch at", i)
		}
	}
	fmt.Println("walk done")
	// ReverseBytes is its own inverse at every width; Reverse twice too.
	// (P3-M9: use intermediates; the frontend miscompiles back-to-back
	// nested f(f(x)) calls in Println — 2026-10-09 frontend bug.)
	var b16 uint16 = 0xABCD
	var b32 uint32 = 0xABCDEF01
	var b64 uint64 = 0xABCDEF0123456789
	rb16 := bits.ReverseBytes16(bits.ReverseBytes16(b16))
	rb32 := bits.ReverseBytes32(bits.ReverseBytes32(b32))
	rb64 := bits.ReverseBytes64(bits.ReverseBytes64(b64))
	r16 := bits.Reverse16(bits.Reverse16(b16))
	fmt.Println(rb16 == b16)
	fmt.Println(rb32 == b32)
	fmt.Println(rb64 == b64)
	fmt.Println(r16 == b16)
}
