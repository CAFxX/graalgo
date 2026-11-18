package main

import "math/bits"

func main() {
	xs := []uint64{0, 1, 2, 3, 0x80, 0xff, 0xdeadbeef, 1 << 63, ^uint64(0), 0x0123456789abcdef}
	for _, x := range xs {
		println(x, bits.LeadingZeros64(x), bits.TrailingZeros64(x), bits.OnesCount64(x), bits.Len64(x),
			bits.Reverse64(x), bits.ReverseBytes64(x), bits.RotateLeft64(x, 13), bits.RotateLeft64(x, -7))
		println(bits.LeadingZeros32(uint32(x)), bits.TrailingZeros16(uint16(x)), bits.OnesCount8(uint8(x)), bits.Len(uint(x)),
			bits.Reverse32(uint32(x)), bits.ReverseBytes16(uint16(x)), bits.RotateLeft8(uint8(x), 3), bits.Reverse8(uint8(x)))
	}
	for i, x := range xs {
		y := xs[len(xs)-1-i]
		s, c := bits.Add64(x, y, 1)
		d, b := bits.Sub64(x, y, 1)
		hi, lo := bits.Mul64(x, y)
		println(s, c, d, b, hi, lo)
		if y != 0 && hi < y {
			q, r := bits.Div64(hi, lo, y)
			println(q, r, bits.Rem64(hi, lo, y))
		}
		h32, l32 := bits.Mul32(uint32(x), uint32(y))
		s32, c32 := bits.Add32(uint32(x), uint32(y), 0)
		println(h32, l32, s32, c32)
	}
	println(bits.UintSize)
	func() {
		defer func() {
			e := recover()
			err, ok := e.(error)
			println("recovered:", ok, err.Error())
			_, isRT := e.(interface{ RuntimeError() })
			println(isRT)
		}()
		bits.Div64(1, 0, 0)
	}()
	func() {
		defer func() { println(recover().(error).Error()) }()
		bits.Div64(5, 0, 3)
	}()
	bits.Div32(7, 0, 2)
}
