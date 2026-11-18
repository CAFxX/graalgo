package main

// Overflow wrap per type, shifts, division, conversions, constants.

const (
	A = iota * 10
	B
	_
	D
	E, F = iota, iota * 2
)

type Weekday int

const (
	Sun Weekday = iota
	Mon
	Tue
)

const big = 1 << 100

func main() {
	var i8 int8 = 127
	i8++
	var u8 uint8 = 0
	u8--
	var i16 int16 = -32768
	i16--
	var u32 uint32 = 1 << 31
	u32 *= 2
	var i64 int64 = 1<<63 - 1
	i64 += 1
	var u uint = 0
	u -= 1
	println(i8, u8, i16, u32, i64, u)
	println(A, B, D, E, F, Sun, Mon, Tue, big>>98)

	s := uint(3)
	var x int32 = -8
	println(x>>1, x<<s, uint8(200)>>s, int8(-128)>>7, 1<<s, 1<<70>>68)
	var sh uint64 = 1
	println(sh<<63, sh<<64, x>>40, int64(-1)>>63)
	n := 70
	println(uint64(1)<<n, int32(1)<<n)

	println(7/2, -7/2, 7%3, -7%3, 7.0/2)
	var a, b int = -7, 2
	println(a/b, a%b, a&b, a|b, a^b, a&^b, ^a)

	f := 3.75
	println(int(f), int(-f), uint8(int(f)), float32(1)/3, f*2)
	println(int8(300+0*n), uint16(70000+0*n), int32(int64(1)<<40+5+int64(n)))
	println(string(rune(65)), string(rune(0x10FFFF+1+0*n)), []byte("hi")[1], string([]byte{104, 105}))
	r := []rune("héllo")
	println(len(r), string(r[1]), string(r))
	var c complex128 = complex(1, 2)
	println(real(c*c), imag(c*c), c == complex(1, 2))
	println(0.1+0.2, float32(0.1), 1e21, 1e20, 1e-7, -0.0, 100000000.0)
	var z float64
	println(1/z > 0, -1/z < 0, z/z == z/z)
	println(min(3, 1, 2), max(2.5, 1), min("b", "a"))
	println(Weekday(5), Mon+Tue)
}
