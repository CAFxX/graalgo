// Generic numeric code instantiated at every integer and float width:
// wraparound, division, shifts and conversions must follow the type
// argument's width inside shared (dictionary/stenciled) bodies. Ends with
// an UNRECOVERED integer divide by zero inside a generic function (exit 2).
package main

import (
	"cmp"
	"slices"
	"strconv"
)

type Int interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Uint interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Float interface{ ~float32 | ~float64 }

type MyI8 int8

func wrapS[T Int](name string, x T) {
	one := T(1)
	max := ^T(0) ^ (T(-1) << (sizeof(x) - 1)) // max positive
	min := -max - 1
	println(name, x, x+max, x-min, x*x, -x, ^x, max+one == min, min/T(-1), min%T(-1), x>>3, x<<5, T(int64(x)*1000))
	for s := uint(0); s < 70; s += 13 {
		println(" ", name, s, x<<s, x>>s, min>>s)
	}
}

func wrapU[T Uint](name string, x T) {
	max := ^T(0)
	println(name, x, x+max, x-max, x*x, -x, ^x, max/3, max%7, x>>1, x<<(sizeof(x)-1), T(uint64(x)*0x10001))
}

func sizeof[T Int | Uint](x T) uint {
	var n uint
	for v := ^T(0); v != 0 && n < 128; v = v << 1 {
		n++
	}
	return n
}

func fl[T Float](name string, a, b T) {
	println(name, a, b, a+b, a-b, a*b, a/b, T(float64(a)*1e30)/T(1e30), a == b, cmp.Compare(a, b), min(a, b), max(a, b))
}

func sum[T Int | Uint | Float](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func divAll[T Int](xs []T, d T) []T {
	out := make([]T, len(xs))
	for i, x := range xs {
		out[i] = x / d
	}
	return out
}

func conv[F, T Int | Uint](x F) T { return T(x) }

func main() {
	wrapS("i8", int8(100))
	wrapS("i8n", int8(-77))
	wrapS("my8", MyI8(-128))
	wrapS("i16", int16(31000))
	wrapS("i32", int32(-2000000000))
	wrapS("i64", int64(9000000000000000000))
	wrapS("int", -5)
	wrapU("u8", uint8(250))
	wrapU("u16", uint16(65000))
	wrapU("u32", uint32(4000000000))
	wrapU("u64", uint64(18000000000000000000))
	wrapU("uint", uint(3))
	wrapU("uptr", uintptr(1<<40))
	fl("f32", float32(0.1), float32(0.2))
	fl("f64", 0.1, 0.2)
	fl("f32b", float32(16777216), float32(1))
	fl("f32c", float32(3.4e38), float32(0.5))
	println("sum8", sum([]int8{100, 100, 100, -50}), sum([]uint8{200, 100}), sum([]int16{30000, 30000}), sum([]float32{1e8, 1, 1, 1, 1}), sum([]float64{1e16, 1, 1}))
	println("conv", conv[int64, int8](300), conv[int8, uint64](-1), conv[uint32, int16](70000), conv[int, uintptr](-1), conv[uint8, int32](255))
	d := divAll([]int16{-32768, 7, -7, 32767}, -1)
	println("div", d[0], d[1], d[2], d[3])
	ss := []int8{5, -3, 127, -128, 0}
	slices.Sort(ss)
	println("sorted", ss[0], ss[1], ss[2], ss[3], ss[4], slices.Max(ss), slices.Min(ss))
	fs := []float64{3, -0.0, 1e-300, -1e300, 2}
	slices.SortFunc(fs, cmp.Compare[float64])
	out := ""
	for _, f := range fs {
		out += strconv.FormatFloat(f, 'g', -1, 64) + " "
	}
	println("fsorted", out)
	var zero int32
	println("about to divide by zero")
	r := divAll([]int32{1, 2, 3}, zero)
	println("unreachable", r[0])
}
