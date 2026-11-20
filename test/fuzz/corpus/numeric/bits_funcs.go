// math/bits counting and permutation functions at every width:
// LeadingZeros, TrailingZeros, OnesCount, Len, RotateLeft (positive and
// negative counts), Reverse, ReverseBytes — on edge patterns routed through
// slices, plus constant-argument calls the compiler may fold.
package main

import "math/bits"

var v64 = []uint64{0, 1, 2, 3, 0x80, 0xff, 0x100, 0x8000, 0xffff, 0x10000, 0x80000000, 0xffffffff, 0x100000000,
	0x8000000000000000, 0xffffffffffffffff, 0x5555555555555555, 0xaaaaaaaaaaaaaaaa, 0x0123456789abcdef, 0xfedcba9876543210,
	0x00ff00ff00ff00ff, 0x7fffffffffffffff, 0x0000000100000001}

var rots = []int{0, 1, -1, 7, -7, 8, 15, 16, 31, 32, 33, 63, 64, 65, -64, -65, 1000, -1000}

func main() {
	for _, x := range v64 {
		println("64", x, bits.LeadingZeros64(x), bits.TrailingZeros64(x), bits.OnesCount64(x), bits.Len64(x), bits.Reverse64(x), bits.ReverseBytes64(x))
		u := uint(x)
		println("uint", bits.LeadingZeros(u), bits.TrailingZeros(u), bits.OnesCount(u), bits.Len(u), bits.Reverse(u), bits.ReverseBytes(u))
		x32 := uint32(x)
		println("32", x32, bits.LeadingZeros32(x32), bits.TrailingZeros32(x32), bits.OnesCount32(x32), bits.Len32(x32), bits.Reverse32(x32), bits.ReverseBytes32(x32))
		x16 := uint16(x)
		println("16", x16, bits.LeadingZeros16(x16), bits.TrailingZeros16(x16), bits.OnesCount16(x16), bits.Len16(x16), bits.Reverse16(x16), bits.ReverseBytes16(x16))
		x8 := uint8(x >> 3)
		println("8", x8, bits.LeadingZeros8(x8), bits.TrailingZeros8(x8), bits.OnesCount8(x8), bits.Len8(x8), bits.Reverse8(x8))
		for _, k := range rots {
			println(" rot", k, bits.RotateLeft64(x, k), bits.RotateLeft32(x32, k), bits.RotateLeft16(x16, k), bits.RotateLeft8(x8, k), bits.RotateLeft(u, k))
		}
	}
	// exhaustive 8- and 16-bit consistency checks against naive versions
	bad := 0
	for i := 0; i < 1<<16; i++ {
		x := uint16(i)
		n := 0
		lz := 16
		tz := 16
		var rev uint16
		for b := 0; b < 16; b++ {
			if x&(1<<b) != 0 {
				n++
				if tz == 16 {
					tz = b
				}
				lz = 15 - b
				rev |= 1 << (15 - b)
			}
		}
		if bits.OnesCount16(x) != n || bits.LeadingZeros16(x) != lz || bits.TrailingZeros16(x) != tz || bits.Reverse16(x) != rev {
			bad++
		}
		if bits.OnesCount32(uint32(x)*0x10001) != 2*n || bits.Len64(uint64(x)<<40) != (16-lz)+40*boolInt(x != 0) {
			bad++
		}
	}
	println("naive bad", bad)
	// constant arguments
	println("const", bits.LeadingZeros64(1), bits.TrailingZeros32(0), bits.OnesCount8(0xff), bits.Len(0), bits.Reverse8(1), bits.RotateLeft8(0x81, -1), bits.ReverseBytes16(0x1234))
	println("UintSize", bits.UintSize)
	// accumulate in a loop to stress the call path
	var acc uint64
	for i := uint64(1); i < 100000; i++ {
		y := i * 0x9e3779b97f4a7c15
		acc += uint64(bits.OnesCount64(y)) + uint64(bits.LeadingZeros64(y)) + bits.RotateLeft64(y, int(i%67)-33)
		acc ^= bits.Reverse64(acc)
	}
	println("acc", acc)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
