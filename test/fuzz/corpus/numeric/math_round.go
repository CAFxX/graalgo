// math rounding and sign functions over a table of edge values: Floor,
// Ceil, Trunc, Round, RoundToEven, Modf, Abs, Copysign, Signbit, Mod,
// Remainder, Dim, Max/Min, FMA — including ±0, ±Inf, NaN, halves,
// 2^52±0.5 boundaries and denormals. Bits printed to expose -0 vs +0.
package main

import (
	"math"
	"strconv"
)

var nz = math.Copysign(0, -1)
var vals = []float64{0, nz, 0.5, -0.5, 1.5, -1.5, 2.5, -2.5, 0.49999999999999994, -0.49999999999999994,
	1, -1, 0.1, -0.9, 3.7, -3.7, 4503599627370495.5, -4503599627370495.5, 4503599627370496, 9007199254740993,
	1e300, -1e300, 5e-324, -5e-324, 2.2250738585072014e-308, math.Inf(1), math.Inf(-1), math.NaN(), 7.5, -7.5, 123456.5}

// fb prints a float with its bit pattern except NaN (sign is hardware-specific).
func fb(x float64) string {
	if x != x {
		return "NaN"
	}
	return strconv.FormatFloat(x, 'g', -1, 64) + "/" + strconv.FormatUint(math.Float64bits(x), 16)
}

func main() {
	for _, x := range vals {
		println("r", fb(x), fb(math.Floor(x)), fb(math.Ceil(x)), fb(math.Trunc(x)), fb(math.Round(x)), fb(math.RoundToEven(x)))
		ip, fp := math.Modf(x)
		println("m", fb(x), fb(ip), fb(fp), fb(math.Abs(x)), fb(-x), math.Signbit(x))
		println("c", fb(math.Copysign(x, -1)), fb(math.Copysign(x, 1)), fb(math.Copysign(3, x)), fb(math.Copysign(x, nz)))
	}
	divs := []float64{1, -1, 0.3, -0.3, 2, 3, 0, nz, math.Inf(1), math.Inf(-1), math.NaN(), 5e-324, 1e300}
	for _, x := range vals {
		for _, y := range divs {
			println("mod", fb(x), fb(y), fb(math.Mod(x, y)), fb(math.Remainder(x, y)), fb(math.Dim(x, y)), fb(math.Max(x, y)), fb(math.Min(x, y)))
		}
	}
	// FMA vs separate ops
	fs := [][3]float64{{0.1, 10, -1}, {1 + 1e-16*2, 1 - 1e-16*2, -1}, {1e308, 10, -1e309 / 10}, {2, 3, 4}, {nz, 1, 0}, {0, 1, nz}, {math.Inf(1), 0, 1}, {5e-324, 0.5, 0}}
	for _, f := range fs {
		println("fma", fb(f[0]), fb(f[1]), fb(f[2]), fb(math.FMA(f[0], f[1], f[2])), fb(float64(f[0]*f[1])+f[2]))
	}
	// Round vs RoundToEven over n+0.5 in a loop
	diff := 0
	for i := -1000; i <= 1000; i++ {
		x := float64(i) + 0.5
		if math.Round(x) != math.RoundToEven(x) {
			diff++
		}
		if math.Floor(-x) != -math.Ceil(x) {
			diff += 1000
		}
	}
	println("halfdiff", diff)
	// floats to int via Floor at large magnitudes
	for e := 50; e <= 54; e++ {
		b := math.Ldexp(1, e)
		for _, d := range []float64{-1.5, -1, -0.5, 0, 0.5, 1, 1.5} {
			x := b + d
			println("big", e, fb(d), fb(x), fb(math.Floor(x)), fb(math.Round(x)), int64(math.Trunc(x)))
		}
	}
	// float32 rounding through math
	for _, x := range []float32{0.5, 1.5, 2.5, -2.5, 8388607.5, 8388608.5, 0.1} {
		println("f32", x, float32(math.Round(float64(x))), float32(math.RoundToEven(float64(x))), float32(math.Floor(float64(x))))
	}
}
