// NaN / ±Inf / ±0 semantics: all six comparisons, println formatting of
// specials for float32 and float64, arithmetic producing specials, -0
// propagation (division sign, Signbit), NaN != NaN in slices/arrays/structs
// equality, and min/max builtins with NaN and signed zeros.
package main

import "math"

type pair struct {
	a float64
	b float32
}

var negz = math.Copysign(0, -1)
var vals = []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, negz, 1, -1, math.MaxFloat64, -math.MaxFloat64,
	math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64, 1e-310, 0.1, 1e308}

// sb reports the sign bit, except for NaN whose sign is hardware-specific.
func sb(x float64) int {
	if x != x {
		return 2
	}
	if math.Signbit(x) {
		return 1
	}
	return 0
}

func cmp6(a, b float64) (bool, bool, bool, bool, bool, bool) {
	return a == b, a != b, a < b, a <= b, a > b, a >= b
}

func cmp6f(a, b float32) (bool, bool, bool, bool, bool, bool) {
	return a == b, a != b, a < b, a <= b, a > b, a >= b
}

func main() {
	for _, a := range vals {
		println("val", a, float32(a), math.Signbit(a), math.IsNaN(a), math.IsInf(a, 0), math.IsInf(a, 1), math.IsInf(a, -1))
		for _, b := range vals {
			e, ne, lt, le, gt, ge := cmp6(a, b)
			fe, fne, flt, fle, fgt, fge := cmp6f(float32(a), float32(b))
			println(" ", a, b, e, ne, lt, le, gt, ge, "|", fe, fne, flt, fle, fgt, fge)
		}
	}
	for _, a := range vals {
		for _, b := range vals[:7] {
			println("ar", a, b, a+b, a-b, a*b, a/b, sb(a*b), sb(a/b))
			x, y := float32(a), float32(b)
			println("ar32", x, y, x+y, x-y, x*y, x/y)
		}
	}
	// -0 corner cases
	z := 0.0
	nz := negz
	println("negzero", nz, -z, z-z, -z-z, nz+nz, nz+z, z*-1, 1/nz, 1/(-z), 1/(nz+z), 1/(nz*nz), 1/(nz-z))
	println("negzero sign", math.Signbit(-z), math.Signbit(z-z), math.Signbit(nz*z), math.Signbit(-nz), math.Signbit(nz-nz))
	var fz float32
	println("negzero32", -fz, 1/-fz, 1/fz, math.Signbit(float64(-fz)))
	// NaN in aggregate equality
	n := math.NaN()
	a1 := [2]float64{n, 1}
	a2 := a1
	println("arrNaN", a1 == a2, a1 != a2)
	p1 := pair{n, 1}
	p2 := pair{1, float32(n)}
	println("structNaN", p1 == p1, p2 == p2, pair{0, 0} == pair{negz, float32(negz)})
	var i1 interface{} = n
	var i2 interface{} = negz
	var i3 interface{} = 0.0
	println("ifaceNaN", i1 == i1, i2 == i3, i2 != i3)
	// min/max builtins
	mm := []float64{n, 1, -1, 0, negz, math.Inf(1), math.Inf(-1)}
	for _, a := range mm {
		for _, b := range mm {
			println("minmax", a, b, min(a, b), max(a, b), sb(min(a, b)), sb(max(a, b)), math.Min(a, b), math.Max(a, b))
		}
	}
	println("min3", min(1.0, n, -1.0), max(negz, 0.0), min(0.0, negz), max(float32(2), float32(n)))
	// counts with NaN in a loop
	cnt := 0
	for _, v := range []float64{n, n, 1, n} {
		if v != v {
			cnt++
		}
		if !(v < 1) && !(v >= 1) {
			cnt += 10
		}
	}
	println("nancount", cnt)
	// Inf arithmetic
	inf := math.Inf(1)
	println("inf", inf-inf, inf*0, inf/inf, -inf+inf, inf+inf, inf*-1, 0/z, z/z, vals[7]*2, vals[8]*2)
	f32max := float32(math.MaxFloat32)
	println("inf32", f32max*2, f32max+f32max, -f32max*2, f32max*0.5, f32max/0.5)
	// println of floats generally
	ps := []float64{1, 10, 100, 1e20, 1e21, 1e-5, 1e-7, 123.456, -0.000123, 5e-324, 2.2250738585072014e-308, 1.7976931348623157e308, 0.3, 1.0 / 3}
	for _, p := range ps {
		println("p", p, float32(p), -p)
	}
}
