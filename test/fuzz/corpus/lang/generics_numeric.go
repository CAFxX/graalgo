// Targets generic numeric code instantiated with many types to catch type
// confusion in a flat value model: ~ and union constraints, overflow
// wrapping per instantiation (int8 vs uint8), float32 rounding, shifts
// T(1)<<s, conversions between type params, and named underlying types.
package main

import (
	"math"
	"strconv"
)

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Float interface{ ~float32 | ~float64 }

type Number interface{ Integer | Float }

type MyU8 uint8
type MyF32 float32

func Sum[T Number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func Mul[T Number](a, b T) T { return a * b }

func Max[T Integer | Float | ~string](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Shl[T Integer](s uint) T { return T(1) << s }

func Shr[T Integer](v T, s uint) T { return v >> s }

func Neg[T Integer](v T) T { return -v }

func Div[T Integer](a, b T) (q, r T) { return a / b, a % b }

func Conv[To, From Number](v From) To { return To(v) }

func AllOnes[T Integer]() T { return ^T(0) }

func Mid[T Integer](a, b T) T { return (a + b) / 2 }

func i64[T Integer](v T) string {
	// print via int64 or uint64 depending on sign of ^T(0)
	if ^T(0) < 0 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatUint(uint64(v), 10)
}

func f64[T Float](v T) string { return strconv.FormatFloat(float64(v), 'g', -1, 64) }

//go:noinline
func n100() int { return 100 }

func main() {
	a := n100()
	println(i64(Sum(int8(a), int8(a))), i64(Sum(uint8(a), uint8(a), uint8(a))), i64(Sum(MyU8(a), MyU8(a), MyU8(a))))
	println(i64(Sum(int16(a*300), int16(a*300))), i64(Sum(uint16(a*300), uint16(a*300))))
	println(i64(Sum(int32(a*30000000), int32(a*30000000))), i64(Sum(uint32(a*30000000), uint32(a*30000000))))
	println(i64(Mul(int8(a), int8(3))), i64(Mul(uint8(a), uint8(3))), i64(Mul(int64(a)<<40, int64(a)<<30)))

	println(i64(Max(int8(-1), int8(1))), i64(Max(uint8(255), uint8(1))), Max("abc", "abd"), Max(-0.0, 0.0))
	println(i64(Max(int8(a+28), int8(-a))), i64(Max(uint8(a+156), uint8(1))))

	for _, s := range []uint{0, 7, 8, 15, 31, 32, 63, 64} {
		println(s, i64(Shl[int8](s)), i64(Shl[uint8](s)), i64(Shl[int16](s)), i64(Shl[uint32](s)), i64(Shl[int64](s)), i64(Shl[uint64](s)))
	}
	println(i64(Shr(int8(-128), 3)), i64(Shr(uint8(128), 3)), i64(Shr(int32(-1), 31)), i64(Shr(uint32(1<<31), 31)), i64(Shr(int64(-8), 70)))

	println(i64(Neg(int8(-128))), i64(Neg(uint8(1))), i64(Neg(uint16(2))), i64(Neg(int32(math.MinInt32))), i64(Neg(uint64(1))))
	q, r := Div(int8(-7), int8(2))
	println(i64(q), i64(r))
	q2, r2 := Div(uint8(250), uint8(7))
	println(i64(q2), i64(r2))
	q3, r3 := Div(int8(-128), Neg(int8(1)))
	println(i64(q3), i64(r3))
	q4, r4 := Div(int64(math.MinInt64), int64(-1))
	println(i64(q4), i64(r4))

	println(i64(AllOnes[int8]()), i64(AllOnes[uint8]()), i64(AllOnes[uint16]()), i64(AllOnes[int]()), i64(AllOnes[uint32]()), i64(AllOnes[MyU8]()))
	println(i64(Mid(uint8(200), uint8(100))), i64(Mid(int8(100), int8(100))), i64(Mid(int(200), int(100))))

	// float32 vs float64 rounding
	third32 := Sum(float32(0.1), float32(0.2), float32(0.3))
	third64 := Sum(0.1, 0.2, 0.3)
	println(f64(third32), f64(third64), f64(Sum(MyF32(1e8), MyF32(1), MyF32(-1e8))), f64(Sum(1e16, 1.0, -1e16)))
	println(f64(Mul(float32(16777217), float32(1))), f64(Mul(float32(1e20), float32(1e20))), f64(Mul(1e200, 1e200)))
	println(f64(Max(float32(math.NaN()), float32(1))), f64(Max(float32(1), float32(math.NaN()))))

	// conversions between type params
	println(i64(Conv[int8](300)), i64(Conv[uint8](int16(-1))), i64(Conv[int32](float32(-2.75))), i64(Conv[uint16](float64(65535.9))))
	println(f64(Conv[float32](int64(16777217))), f64(Conv[float64](uint64(1<<63))), f64(Conv[float32](float64(0.1))))
	println(i64(Conv[int64](MyU8(200))), i64(Conv[MyU8](int(-56))), f64(Conv[MyF32](uint8(255))))

	// a generic accumulator struct per type
	println(acc[int8](20), acc[uint8](20), acc[int16](20), acc[float32](20), acc[float64](20))
}

func acc[T Number](n int) string {
	var x T = 1
	for i := 0; i < n; i++ {
		x = x*3 + 1
	}
	var probe T = 1
	probe /= 2 // 0 for integers, 0.5 for floats
	if probe == 0 {
		var z T
		if ^int64(0) < 0 && z-1 < z {
			return "s" + strconv.FormatFloat(float64(x), 'g', -1, 64)
		}
		return "u" + strconv.FormatFloat(float64(x), 'g', -1, 64)
	}
	return "f" + strconv.FormatFloat(float64(x), 'g', -1, 64)
}
