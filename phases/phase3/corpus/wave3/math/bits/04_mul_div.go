package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// Mul: 2-target desugar, full-width products, max values.
	hi, lo := bits.Mul(0, 0)
	fmt.Println(hi, lo)
	hi, lo = bits.Mul(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF)
	fmt.Printf("%x %x\n", hi, lo)
	hi, lo = bits.Mul(0x100000000, 0x100000000)
	fmt.Printf("%x %x\n", hi, lo)
	hi32, lo32 := bits.Mul32(0xFFFFFFFF, 0xFFFFFFFF)
	fmt.Printf("%x %x\n", hi32, lo32)
	hi64, lo64 := bits.Mul64(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF)
	fmt.Printf("%x %x\n", hi64, lo64)
	hi64, lo64 = bits.Mul64(0x123456789ABCDEF0, 0x0FEDCBA987654321)
	fmt.Printf("%x %x\n", hi64, lo64)
	// Mul identity: Mul(x, 1) -> (0, x).
	hi, lo = bits.Mul(0x123456789ABCDEF0, 1)
	fmt.Println(hi == 0, lo == 0x123456789ABCDEF0)
	// Div: 2-target desugar, exact division, remainder, hi == 0 fast path.
	q, r := bits.Div(0, 100, 7)
	fmt.Println(q, r)
	q, r = bits.Div(1, 0, 3)
	fmt.Printf("%x %x\n", q, r)
	q32, r32 := bits.Div32(0, 0xFFFFFFFF, 0x10000)
	fmt.Printf("%x %x\n", q32, r32)
	// Div64 long path (hi != 0): exercises the Knuth D4 loop incl. the
	// qhat correction loops.
	q64, r64 := bits.Div64(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF)
	fmt.Printf("%x %x\n", q64, r64)
	q64, r64 = bits.Div64(0x123456789ABCDEF0, 0x0FEDCBA987654321, 0x1111111111111111)
	fmt.Printf("%x %x\n", q64, r64)
	q64, r64 = bits.Div64(0, 0xFFFFFFFFFFFFFFFF, 0x100000001)
	fmt.Printf("%x %x\n", q64, r64)
	// Div/Mul round-trip: Div(Mul(x, y)...) reconstructs x (y != 0).
	x, y := uint(0x123456789ABCDEF0), uint(0x11111111)
	hi, lo = bits.Mul(x, y)
	q, r = bits.Div(hi, lo, y)
	fmt.Println(q == x, r == 0)
	// Rem: single-result, incl. the hi%y scaling path in Rem64.
	fmt.Println(bits.Rem(0, 100, 7))
	fmt.Println(bits.Rem32(0, 0xFFFFFFFF, 0x10000))
	fmt.Printf("%x\n", bits.Rem64(0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 1000000007))
	// Rem64 with hi >= y (no quotient-overflow panic, unlike Div).
	fmt.Printf("%x\n", bits.Rem64(0xFFFFFFFFFFFFFFFF, 0, 3))
	// Div then Rem consistency: (hi,lo) = q*y + r with r < y.
	q64, r64 = bits.Div64(0xFEDCBA9876543210, 0x123456789ABCDEF0, 0xABCDEF)
	check := r64 < 0xABCDEF
	hi2, lo2 := bits.Mul64(q64, 0xABCDEF)
	s, _ := bits.Add64(lo2, r64, 0)
	fmt.Println(check, hi2 == 0xFEDCBA9876543210, s == 0x123456789ABCDEF0)
}
