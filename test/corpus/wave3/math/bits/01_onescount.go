package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// OnesCount: zeros, ones, alternating patterns, single bits, max.
	for _, x := range []uint{0, 1, 0xFFFFFFFFFFFFFFFF, 0x5555555555555555, 0xAAAAAAAAAAAAAAAA, 0x8000000000000001, 0x0F0F0F0F0F0F0F0F} {
		fmt.Println(bits.OnesCount(x))
	}
	fmt.Println(bits.OnesCount8(0), bits.OnesCount8(0xFF), bits.OnesCount8(0x55))
	fmt.Println(bits.OnesCount16(0), bits.OnesCount16(0xFFFF), bits.OnesCount16(0x5555))
	fmt.Println(bits.OnesCount32(0), bits.OnesCount32(0xFFFFFFFF), bits.OnesCount32(0x55555555))
	fmt.Println(bits.OnesCount64(0), bits.OnesCount64(0xFFFFFFFFFFFFFFFF), bits.OnesCount64(0x5555555555555555))
	// Cross-check: OnesCount(x) + OnesCount(^x) == width, for each width.
	var x8 uint8 = 0xA5
	var x16 uint16 = 0xA55A
	var x32 uint32 = 0xA55AA55A
	var x64 uint64 = 0xA55AA55AA55AA55A
	fmt.Println(bits.OnesCount8(x8)+bits.OnesCount8(^x8) == 8)
	fmt.Println(bits.OnesCount16(x16)+bits.OnesCount16(^x16) == 16)
	fmt.Println(bits.OnesCount32(x32)+bits.OnesCount32(^x32) == 32)
	fmt.Println(bits.OnesCount64(x64)+bits.OnesCount64(^x64) == 64)
}
