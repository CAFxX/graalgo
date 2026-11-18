package main

import (
	"math"
	"math/cmplx"
)

var xs = []float64{0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 1.5, 2.5, -2.5, 3.14159, 1e-300, 1e300, 123456.789, -7.25e-5,
	math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), math.NaN(), 709.5, -745.2, 1e-320, 4503599627370497.5}

func main() {
	for _, x := range xs {
		println(x, math.Float64bits(x), math.Abs(x), math.Floor(x), math.Ceil(x), math.Trunc(x), math.Round(x), math.RoundToEven(x))
		println(math.Sqrt(x), math.Cbrt(x), math.Exp(x), math.Exp2(x), math.Expm1(x), math.Log(x), math.Log2(x), math.Log10(x), math.Log1p(x))
		println(math.Sin(x), math.Cos(x), math.Tan(x), math.Asin(x), math.Acos(x), math.Atan(x), math.Sinh(x), math.Cosh(x), math.Tanh(x))
		println(math.Asinh(x), math.Acosh(x), math.Atanh(x), math.Gamma(x), math.Erf(x), math.Erfc(x), math.J0(x), math.Y1(x), math.Erfinv(x))
		f, e := math.Frexp(x)
		ip, fp := math.Modf(x)
		lg, sg := math.Lgamma(x)
		s, c := math.Sincos(x)
		println(f, e, ip, fp, lg, sg, s, c, math.Ilogb(x), math.Logb(x), math.Signbit(x), math.IsNaN(x), math.IsInf(x, 0))
		f32 := float32(x)
		println(f32, math.Float32bits(f32), math.Float32frombits(math.Float32bits(f32)+1), math.Nextafter32(f32, 0), math.Nextafter(x, 1))
		for _, y := range []float64{0, 2, -3, 0.5, math.Inf(1), math.NaN(), 1e10} {
			println(math.Pow(x, y), math.Mod(x, y), math.Remainder(x, y), math.Max(x, y), math.Min(x, y), math.Dim(x, y),
				math.Hypot(x, y), math.Atan2(x, y), math.Copysign(x, y), math.FMA(x, y, 1.5), math.Ldexp(x, int(math.Min(math.Abs(y), 50))))
		}
	}
	println(math.Pi, math.E, math.Sqrt2, math.MaxInt64, math.MaxUint32, math.MaxFloat32, math.Float64frombits(0x7ff8000000000001))
	println(math.Pow10(5), math.Pow10(-400), math.Pow10(400), math.Jn(3, 2.5), math.Yn(2, 2.5), math.J1(1.5), math.Y0(0.5))
	for _, z := range []complex128{0, 1, 1i, -1 + 2i, 3.5 - 0.25i, complex(math.Inf(1), 1), cmplx.NaN()} {
		println(cmplx.Abs(z), cmplx.Phase(z), real(cmplx.Sqrt(z)), imag(cmplx.Exp(z)), cmplx.IsNaN(z), cmplx.IsInf(z))
		println(cmplx.Sin(z), cmplx.Cos(z), cmplx.Log(z), cmplx.Pow(z, 2+1i), cmplx.Conj(z), cmplx.Tanh(z), cmplx.Asin(z), cmplx.Atan(z))
		r, th := cmplx.Polar(z)
		println(r, th, cmplx.Rect(r, th), cmplx.Log10(z), cmplx.Cot(z), cmplx.Acosh(z))
	}
}
