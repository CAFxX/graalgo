package main

import (
	"fmt"
	"math/bits"
)

func main() {
	// RotateLeft: k = 0, k = width, k > width, negative k, single bit walk.
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, 1))
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, 0))
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, 64))
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, 65))
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, -1))
	fmt.Printf("%x\n", bits.RotateLeft(0x8000000000000001, -63))
	fmt.Printf("%x\n", bits.RotateLeft8(0x81, 1))
	fmt.Printf("%x\n", bits.RotateLeft8(0x81, -1))
	fmt.Printf("%x\n", bits.RotateLeft8(0x81, 9))
	fmt.Printf("%x\n", bits.RotateLeft16(0x8001, 4))
	fmt.Printf("%x\n", bits.RotateLeft16(0x8001, -4))
	fmt.Printf("%x\n", bits.RotateLeft32(0x80000001, 31))
	fmt.Printf("%x\n", bits.RotateLeft32(0x80000001, -31))
	fmt.Printf("%x\n", bits.RotateLeft64(0x8000000000000001, 63))
	// RotateLeft by k then by -k is the identity.
	x := uint(0x123456789ABCDEF0)
	fmt.Println(bits.RotateLeft(bits.RotateLeft(x, 17), -17) == x)
	// Reverse: bit-reversal involution + known values.
	fmt.Printf("%x\n", bits.Reverse(0x8000000000000000))
	fmt.Printf("%x\n", bits.Reverse(1))
	fmt.Printf("%x\n", bits.Reverse(0xFFFFFFFFFFFFFFFF))
	fmt.Println(bits.Reverse(bits.Reverse(x)) == x)
	fmt.Printf("%x\n", bits.Reverse8(0x12))
	fmt.Printf("%x\n", bits.Reverse8(0xFF))
	fmt.Printf("%x\n", bits.Reverse16(0x1234))
	fmt.Printf("%x\n", bits.Reverse32(0x12345678))
	fmt.Printf("%x\n", bits.Reverse64(0x123456789ABCDEF0))
	// ReverseBytes: byte-swap involution + known values.
	fmt.Printf("%x\n", bits.ReverseBytes(0x0102030405060708))
	fmt.Println(bits.ReverseBytes(bits.ReverseBytes(x)) == x)
	fmt.Printf("%x\n", bits.ReverseBytes16(0x1234))
	fmt.Printf("%x\n", bits.ReverseBytes32(0x12345678))
	fmt.Printf("%x\n", bits.ReverseBytes64(0x123456789ABCDEF0))
	// Combination: Reverse(ReverseBytes(x)) == ReverseBytes(Reverse(x)).
	fmt.Println(bits.Reverse(bits.ReverseBytes(x)) == bits.ReverseBytes(bits.Reverse(x)))
}
