package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// UintSize pin (64 on the linux/amd64 differential target).
	fmt.Println(bits.UintSize)
	// LeadingZeros / Len edge values: 0, 1, powers of two, all-ones, max.
	for _, x := range []uint{0, 1, 2, 0x8000000000000000, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFF00000000, 0x00000000FFFFFFFF} {
		fmt.Println(bits.LeadingZeros(x), bits.Len(x))
	}
	fmt.Println(bits.LeadingZeros8(0), bits.LeadingZeros8(1), bits.LeadingZeros8(0x80), bits.LeadingZeros8(0xFF))
	fmt.Println(bits.LeadingZeros16(0), bits.LeadingZeros16(0x100), bits.LeadingZeros16(0x8000), bits.LeadingZeros16(0xFFFF))
	fmt.Println(bits.LeadingZeros32(0), bits.LeadingZeros32(1), bits.LeadingZeros32(0x80000000), bits.LeadingZeros32(0xFFFFFFFF))
	fmt.Println(bits.LeadingZeros64(0), bits.LeadingZeros64(1), bits.LeadingZeros64(0x8000000000000000), bits.LeadingZeros64(0xFFFFFFFFFFFFFFFF))
	// TrailingZeros edge values (de Bruijn paths, incl. x == 0).
	for _, x := range []uint{0, 1, 2, 3, 0x8000000000000000, 0xFFFFFFFFFFFFFFFF, 0x100000000} {
		fmt.Println(bits.TrailingZeros(x))
	}
	fmt.Println(bits.TrailingZeros8(0), bits.TrailingZeros8(0x10), bits.TrailingZeros8(0x80), bits.TrailingZeros8(0xFF))
	fmt.Println(bits.TrailingZeros16(0), bits.TrailingZeros16(0x1000), bits.TrailingZeros16(0x8000))
	fmt.Println(bits.TrailingZeros32(0), bits.TrailingZeros32(0x10000000), bits.TrailingZeros32(0x80000000))
	fmt.Println(bits.TrailingZeros64(0), bits.TrailingZeros64(0x1000000000000000), bits.TrailingZeros64(0x8000000000000000))
	// Len8/16/32/64 named-result paths.
	fmt.Println(bits.Len8(0), bits.Len8(0xFF))
	fmt.Println(bits.Len16(0), bits.Len16(0x100), bits.Len16(0xFFFF))
	fmt.Println(bits.Len32(0), bits.Len32(0xFFFFFFFF))
	fmt.Println(bits.Len64(0), bits.Len64(0xFFFFFFFFFFFFFFFF))
}
