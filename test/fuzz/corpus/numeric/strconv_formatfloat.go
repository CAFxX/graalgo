// strconv.FormatFloat over formats 'e','E','f','g','G','b','x','X' and
// precisions -1..20 (plus large ones) at bitSize 32 and 64: shortest
// round-trip, rounding of halfway decimals, denormals, ±0, ±Inf, NaN,
// float32 max/min, and AppendFloat. Also round-trips through ParseFloat.
package main

import (
	"math"
	"strconv"
)

var vals = []float64{0, math.Copysign(0, -1), 1, -1, 0.1, 0.5, 1.5, 2.5, 0.125, 0.0625, 1.0 / 3, 2.0 / 3, 123456789, 1e21, 1e20, 1e-7, 1e-6,
	5e-324, 2.2250738585072014e-308, math.MaxFloat64, 1e23, 9007199254740993, 0.000001234, 100, 999.9999, 9.5, 0.95, 0.045, 1.005,
	math.Inf(1), math.Inf(-1), math.NaN(), math.MaxFloat32, math.SmallestNonzeroFloat32, 3.4028235e38, 16777217, 1.1754943508222875e-38,
	-123.456e-10, 6.02214076e23, math.Pi}

var fmts = []byte{'e', 'E', 'f', 'g', 'G', 'b', 'x', 'X'}

func main() {
	for _, v := range vals {
		for _, f := range fmts {
			for prec := -1; prec <= 20; prec++ {
				if f == 'f' && math.Abs(v) > 1e30 && prec > 3 {
					continue
				}
				s64 := strconv.FormatFloat(v, f, prec, 64)
				s32 := strconv.FormatFloat(v, f, prec, 32)
				println(string(f), prec, s64, s32)
			}
		}
		for _, prec := range []int{25, 40, 60} {
			println("big", prec, strconv.FormatFloat(v, 'e', prec, 64), strconv.FormatFloat(v, 'g', prec, 64))
		}
		println("fbig", strconv.FormatFloat(v, 'f', 30, 64))
		if math.Abs(v) < 1e30 {
			println("f330", strconv.FormatFloat(v, 'f', -1, 64), strconv.FormatFloat(v, 'f', 330, 64)[:40])
		}
		b := strconv.AppendFloat([]byte("v:"), v, 'g', -1, 32)
		println("append", string(b))
		// round trip: shortest repr must parse back exactly
		s := strconv.FormatFloat(v, 'g', -1, 64)
		back, err := strconv.ParseFloat(s, 64)
		ok := back == v || (v != v && back != back)
		println("rt64", s, ok, err == nil)
		s = strconv.FormatFloat(v, 'g', -1, 32)
		back, err = strconv.ParseFloat(s, 32)
		ok = float32(back) == float32(v) || (v != v && back != back)
		println("rt32", s, ok, err == nil)
	}
	// unusual format byte and halfway rounding sweep
	println("fmt?", strconv.FormatFloat(1.5, 'z', 2, 64), strconv.FormatFloat(1.5, 'v', -1, 64))
	for i := 0; i < 40; i++ {
		x := float64(i)/8 + 0.0625
		println("half", x, strconv.FormatFloat(x, 'f', 3, 64), strconv.FormatFloat(x, 'f', 2, 64), strconv.FormatFloat(x, 'f', 1, 64), strconv.FormatFloat(x, 'f', 0, 64))
	}
	// powers of ten boundaries
	for e := -12; e <= 25; e++ {
		x := math.Pow(10, float64(e))
		println("p10", e, strconv.FormatFloat(x, 'g', -1, 64), strconv.FormatFloat(x, 'g', -1, 32), strconv.FormatFloat(x, 'G', 3, 64), strconv.FormatFloat(x*0.9999999, 'g', 6, 64))
	}
	// float32 neighbours
	f := float32(1)
	for i := 0; i < 10; i++ {
		f = math.Nextafter32(f, 2)
		println("n32", strconv.FormatFloat(float64(f), 'g', -1, 32), strconv.FormatFloat(float64(f), 'g', -1, 64), strconv.FormatFloat(float64(f), 'b', -1, 32))
	}
}
