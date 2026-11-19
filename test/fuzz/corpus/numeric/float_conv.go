// Float conversions: float64->float32 rounding (ties-to-even, overflow to
// Inf, denormals), int64/uint64->float64/float32 rounding (incl. uint64
// above 2^63), in-range float->int truncation toward zero for every width,
// and double rounding float64->float32->int.
package main

import "math"

var f64s = []float64{0, 1, -1, 0.1, 0.5, 1.5, 2.5, -2.5, 0.49999999999999994, 1e10, 123456789.987,
	16777216, 16777217, 16777218, 16777219, 33554435, 1.0000000596046448, 1.0000000596046449,
	3.4028234663852886e38, 3.4028235677973366e38, 3.402823567797337e38, 1e39, -1e39,
	1e-45, 1.401298464324817e-45, 7e-46, 7.006492321624086e-46, 7.006492321624087e-46, 1.1754942e-38,
	math.SmallestNonzeroFloat64, math.MaxFloat64, math.Inf(1), math.Inf(-1), math.Copysign(0, -1),
	2147483647.9, -2147483648.9, 9007199254740993, 9.223372036854775e18}

var i64s = []int64{0, 1, -1, 1<<24 + 1, 1<<53 + 1, 1<<53 + 2, 1<<53 + 3, 1<<62 + 1<<9, 1<<62 + 1<<9 + 1,
	9223372036854775807, -9223372036854775808, -9223372036854775807, 0x7ffffe8000000000, 0x7fffff4000000000}

var u64s = []uint64{0, 1, 1 << 63, 1<<63 + 1, 1<<63 + 1<<10, 1<<63 + 1<<10 + 1, 1<<63 + 3<<10, 18446744073709551615,
	18446744073709549568, 18446744073709550591, 18446744073709550592, 0x8000008000000000, 0x8000018000000001}

func f32(x float64) float32 { return float32(x) }

func main() {
	for _, x := range f64s {
		y := f32(x)
		println("f64->f32", x, y, float64(y), math.Float32bits(y), math.Float64bits(float64(y)))
	}
	for _, x := range i64s {
		println("i64->f", x, float64(x), float32(x), math.Float64bits(float64(x)), math.Float32bits(float32(x)))
	}
	for _, x := range u64s {
		println("u64->f", x, float64(x), float32(x), math.Float64bits(float64(x)), math.Float32bits(float32(x)))
	}
	ins := []float64{0, 0.9, -0.9, 1.5, -1.5, 99.99, -99.99, 127.9, -128.9, 255.9, 32767.5, -32768.5, 65535.9,
		2147483647.5, -2147483648.5, 4294967295.5, 1e15 + 0.5, -1e15 - 0.5, 9.223372036854775e18, -9.223372036854775807e18,
		math.Copysign(0, -1), 5e-324, -5e-324}
	for _, x := range ins {
		if x > -129 && x < 128 {
			println("i8", x, int8(x))
		}
		if x > -1 && x < 256 {
			println("u8", x, uint8(x))
		}
		if x > -32769 && x < 32768 {
			println("i16", x, int16(x))
		}
		if x > -1 && x < 65536 {
			println("u16", x, uint16(x))
		}
		if x > -2147483649 && x < 2147483648 {
			println("i32", x, int32(x))
		}
		if x > -1 && x < 4294967296 {
			println("u32", x, uint32(x))
		}
		if x >= -9223372036854775808 && x < 9223372036854775808 {
			println("i64", x, int64(x), int(x))
		}
		if x > -1 && x < 18446744073709551616 {
			println("u64", x, uint64(x), uint(x), uintptr(x))
		}
	}
	us := []float64{9223372036854775808, 9223372036854777856, 1.8446744073709550e19, 1.3835058055282164e19, 1e19}
	for _, x := range us {
		println("bigu", x, uint64(x))
	}
	// float32 -> int, via float32 rounding first
	dr := []float64{16777217.0, 16777219.0, 2147483583.0, 2147483584.0, 1.00000005e9, 0.99999999, 8388608.5, 8388609.5, -8388609.5}
	for _, x := range dr {
		y := float32(x)
		println("double", x, y, int64(y), int32(x), int64(float64(y)))
	}
	// float32 -> float64 exact
	f32s := []float32{0.1, 1.0 / 3, 3.4028235e38, 1.4e-45, 1.17549435e-38, -0.0, 16777216}
	for _, y := range f32s {
		println("f32->f64", y, float64(y), math.Float32bits(y))
	}
	// int -> float32 in a loop: count exact
	exact := 0
	for i := int32(16777000); i < 16777600; i++ {
		if int32(float32(i)) == i {
			exact++
		}
	}
	println("exact", exact)
	var sum float32
	for i := 0; i < 1000; i++ {
		sum += float32(i) * 0.1
	}
	println("sum", sum, int(sum), int32(sum*1000))
}
