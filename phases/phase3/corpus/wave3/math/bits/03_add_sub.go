package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// Add: 2-target desugar, carry propagation, carry-in 0/1, max values.
	s, c := bits.Add(1, 2, 0)
	fmt.Println(s, c)
	s, c = bits.Add(0xFFFFFFFFFFFFFFFF, 1, 0)
	fmt.Println(s, c)
	s, c = bits.Add(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 1)
	fmt.Println(s, c)
	s, c = bits.Add(0, 0, 1)
	fmt.Println(s, c)
	s, c = bits.Add(0x8000000000000000, 0x8000000000000000, 0)
	fmt.Println(s, c)
	// Add32/Add64: same carry logic at fixed widths.
	s32, c32 := bits.Add32(0xFFFFFFFF, 1, 0)
	fmt.Println(s32, c32)
	s32, c32 = bits.Add32(0xFFFFFFFF, 0xFFFFFFFF, 1)
	fmt.Println(s32, c32)
	s64, c64 := bits.Add64(0xFFFFFFFFFFFFFFFF, 1, 0)
	fmt.Println(s64, c64)
	s64, c64 = bits.Add64(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 1)
	fmt.Println(s64, c64)
	// Add identity: (s, c) = Add(x, 0, 0) -> s == x, c == 0.
	s, c = bits.Add(0x123456789ABCDEF0, 0, 0)
	fmt.Println(s == 0x123456789ABCDEF0, c == 0)
	// Sub: 2-target desugar, borrow propagation, borrow-in 0/1.
	d, b := bits.Sub(3, 1, 0)
	fmt.Println(d, b)
	d, b = bits.Sub(0, 1, 0)
	fmt.Printf("%x %d\n", d, b)
	d, b = bits.Sub(0, 0, 1)
	fmt.Printf("%x %d\n", d, b)
	d, b = bits.Sub(0x8000000000000000, 1, 0)
	fmt.Printf("%x %d\n", d, b)
	d32, b32 := bits.Sub32(0, 1, 0)
	fmt.Printf("%x %d\n", d32, b32)
	d32, b32 = bits.Sub32(0x80000000, 1, 1)
	fmt.Printf("%x %d\n", d32, b32)
	d64, b64 := bits.Sub64(0, 1, 0)
	fmt.Printf("%x %d\n", d64, b64)
	// Sub identity: (d, b) = Sub(x, 0, 0) -> d == x, b == 0.
	d, b = bits.Sub(0x123456789ABCDEF0, 0, 0)
	fmt.Println(d == 0x123456789ABCDEF0, b == 0)
	// Add/Sub round-trip: Sub(Add(x,y,0)...) reconstructs.
	x, y := uint(0xDEADBEEFCAFEBABE), uint(0x1234567890ABCDEF)
	s, c = bits.Add(x, y, 0)
	d, b = bits.Sub(s, y, 0)
	fmt.Println(d == x, b == c)
}
