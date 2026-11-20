// math transcendental and bit-level functions on special and boundary
// inputs, printed bit-exactly: Frexp/Ldexp (denormals, overflow), Float64bits
// round trips, Nextafter/Nextafter32, Sqrt, Cbrt, Pow special cases, Hypot,
// Log/Log2/Log10/Log1p, Exp/Exp2/Expm1, Ilogb/Logb, Atan2, Sin/Cos/Tan.
package main

import (
	"math"
	"strconv"
)

var nz = math.Copysign(0, -1)
var inf = math.Inf(1)

func fb(x float64) string {
	if x != x {
		return "NaN"
	}
	return strconv.FormatFloat(x, 'g', -1, 64) + "/" + strconv.FormatUint(math.Float64bits(x), 16)
}

var xs = []float64{0, nz, 1, -1, 0.5, 2, 10, 0.1, -0.1, 1e-300, 5e-324, 2.2250738585072014e-308, 2.225073858507201e-308,
	1e300, math.MaxFloat64, inf, -inf, math.NaN(), math.Pi, math.E, 709.782712893384, 709.79, -745.13321910194110842, -746,
	1e-17, 3, 100, 1.0000000000000002}

func main() {
	for _, x := range xs {
		fr, e := math.Frexp(x)
		println("frexp", fb(x), fb(fr), e, fb(math.Ldexp(fr, e)), math.Ilogb(x), fb(math.Logb(x)))
		println("next", fb(math.Nextafter(x, inf)), fb(math.Nextafter(x, -inf)), fb(math.Nextafter(x, 0)), fb(math.Nextafter(x, x)))
		println("sqrt", fb(math.Sqrt(x)), fb(math.Cbrt(x)), fb(math.Sqrt(x)*math.Sqrt(x)))
		println("log", fb(math.Log(x)), fb(math.Log2(x)), fb(math.Log10(x)), fb(math.Log1p(x)))
		println("exp", fb(math.Exp(x)), fb(math.Exp2(x)), fb(math.Expm1(x)), fb(math.Exp(-x)))
		println("trig", fb(math.Sin(x)), fb(math.Cos(x)), fb(math.Tan(x)), fb(math.Atan(x)), fb(math.Tanh(x)))
		println("bits", math.Float64bits(x) == math.Float64bits(math.Float64frombits(math.Float64bits(x))), math.Float32bits(float32(x)))
	}
	ldx := []int{-1080, -1075, -1074, -1073, -1023, -1022, -1, 0, 1, 52, 1023, 1024, 2000, -2000}
	for _, e := range ldx {
		println("ldexp", e, fb(math.Ldexp(1, e)), fb(math.Ldexp(0.75, e)), fb(math.Ldexp(-1.5, e)), fb(math.Ldexp(5e-324, e+1074)))
	}
	pw := []float64{0, nz, 1, -1, 0.5, -0.5, 2, -2, 3, -3, 1.5, inf, -inf, math.NaN(), 1e10, 0.1}
	for _, b := range pw {
		for _, e := range pw {
			println("pow", fb(b), fb(e), fb(math.Pow(b, e)))
		}
	}
	for _, a := range pw {
		for _, b := range pw[:12] {
			println("hyp", fb(a), fb(b), fb(math.Hypot(a, b)), fb(math.Atan2(a, b)))
		}
	}
	println("hypbig", fb(math.Hypot(1e300, 1e300)), fb(math.Hypot(3e-320, 4e-320)), fb(math.Hypot(inf, math.NaN())))
	// Nextafter32
	f32 := []float32{0, 1, -1, math.MaxFloat32, math.SmallestNonzeroFloat32, float32(math.Inf(1)), 1.17549435e-38}
	for _, x := range f32 {
		a := math.Nextafter32(x, float32(inf))
		b := math.Nextafter32(x, float32(-inf))
		println("next32", x, a, b, math.Float32bits(a), math.Float32bits(b))
	}
	// Float64frombits patterns
	pats := []uint64{0, 1, 0x000fffffffffffff, 0x0010000000000000, 0x3ff0000000000001, 0x7fefffffffffffff, 0x7ff0000000000000,
		0x8000000000000000, 0xbff0000000000000, 0xfff0000000000000, 0x4340000000000000, 0x3fb999999999999a}
	for _, p := range pats {
		f := math.Float64frombits(p)
		println("frombits", strconv.FormatUint(p, 16), f, fb(f), float32(f), math.Float32bits(float32(f)))
	}
	p32 := []uint32{0, 1, 0x007fffff, 0x00800000, 0x3f800001, 0x7f7fffff, 0x7f800000, 0x80000000, 0xff800000, 0x3dcccccd}
	for _, p := range p32 {
		f := math.Float32frombits(p)
		println("frombits32", p, f, float64(f), math.Float32bits(f) == p)
	}
	// compound: sum of logs vs log of product
	s := 0.0
	prod := 1.0
	for i := 1; i <= 170; i++ {
		s += math.Log(float64(i))
		prod *= float64(i)
	}
	println("lgamma-ish", fb(s), fb(math.Log(prod)), fb(prod))
	lg, sign := math.Lgamma(171)
	println("lgamma", fb(lg), sign, fb(math.Gamma(171)), fb(math.Gamma(172)), fb(math.Gamma(-1.5)), fb(math.Gamma(0.5)))
	println("erf", fb(math.Erf(0.5)), fb(math.Erfc(10)), fb(math.Erfinv(0.5)), fb(math.J0(1)), fb(math.Y0(1)))
	println("hyper", fb(math.Sinh(710)), fb(math.Cosh(-710)), fb(math.Asinh(1e300)), fb(math.Acosh(1)), fb(math.Atanh(1)), fb(math.Atanh(-1)), fb(math.Atanh(2)))
	println("inv", fb(math.Asin(1)), fb(math.Acos(-1)), fb(math.Asin(2)), fb(math.Acos(1.0000000000000002)))
	sn, cs := math.Sincos(1e22)
	println("sincos", fb(sn), fb(cs), fb(math.Sin(1e300)), fb(math.Cos(math.MaxFloat64)))
}
