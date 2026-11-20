// Targets bit-exact elementary math: Sin/Cos/Tan/Sincos for huge arguments
// (Payne-Hanek reduction), Atan2 in all quadrants and signed zeros,
// Expm1/Log1p/Exp/Exp2/Log/Log2/Log10/Cbrt/Pow/Hypot/Mod/Remainder/Frexp/
// Ldexp/Modf/Nextafter, math/bits ops and math/cmplx; printed via Float64bits.
package main

import (
	"math"
	"math/bits"
	"math/cmplx"
)

var h uint64

func reset() { h = 14695981039346656037 }

func mix(f float64) {
	b := math.Float64bits(f)
	if f != f {
		b = 1
	}
	h = (h ^ b) * 1099511628211
}

func fb(f float64) uint64 {
	if f != f {
		return 1
	}
	return math.Float64bits(f)
}

func main() {
	nan := math.NaN()
	inf := math.Inf(1)
	negz := math.Copysign(0, -1)
	huge := []float64{1e10, 1e15, 1 << 29, 1 << 30, 1 << 52, 1 << 53, 1e19, 1e22, 1e100, 1e300, math.MaxFloat64, 3.14159265358979e200, -1e18, 6.0e17, 2.5e308 / 2}
	for _, x := range huge {
		s, c := math.Sincos(x)
		println(fb(x), fb(math.Sin(x)), fb(math.Cos(x)), fb(math.Tan(x)), fb(s), fb(c))
	}
	reset()
	x := 0.3
	for i := 0; i < 2000; i++ {
		x *= 1.37
		if x > 1e300 {
			x = x/1e299 + float64(i)
		}
		mix(math.Sin(x))
		mix(math.Cos(-x))
		mix(math.Tan(x))
	}
	println("trig-huge", h)
	reset()
	for i := -5000; i <= 5000; i++ {
		y := float64(i) * 0.0123
		mix(math.Sin(y))
		mix(math.Cos(y))
		mix(math.Tan(y))
		mix(math.Asin(y / 62))
		mix(math.Acos(y / 62))
		mix(math.Atan(y))
		mix(math.Sinh(y / 10))
		mix(math.Cosh(y / 10))
		mix(math.Tanh(y / 10))
		mix(math.Asinh(y))
		mix(math.Acosh(1 + math.Abs(y)))
		mix(math.Atanh(y / 62))
	}
	println("trig-small", h)

	pts := []float64{nan, inf, -inf, 0, negz, 1, -1, 1e-310, -1e300}
	for _, y := range pts {
		for _, x := range pts {
			print(fb(math.Atan2(y, x))%100000, " ")
		}
		println()
	}
	reset()
	for i := 0; i < 360; i++ {
		a := float64(i) * math.Pi / 180
		mix(math.Atan2(math.Sin(a), math.Cos(a)))
		mix(math.Atan2(-math.Sin(a)*1e-300, math.Cos(a)*1e-300))
	}
	println("atan2", h)

	reset()
	for i := -4000; i <= 4000; i++ {
		v := float64(i) / 997
		mix(math.Expm1(v))
		mix(math.Log1p(v * 0.999 / 4.02))
		mix(math.Exp(v * 30))
		mix(math.Exp2(v * 40))
		mix(math.Cbrt(v * 1e5))
		mix(math.Log(math.Abs(v) + 1e-9))
		mix(math.Log2(math.Abs(v) * 1e10))
		mix(math.Log10(math.Abs(v) * 1e-10))
		mix(math.Sqrt(math.Abs(v)))
	}
	println("explog", h)
	for _, v := range []float64{1e-20, -1e-20, 1e-5, 0.5, -0.5, 709.78, 709.79, -745.2, -745.1, 1024, -1075, 1e300} {
		print(fb(math.Expm1(v)), " ", fb(math.Exp(v)), " ", fb(math.Exp2(v)), " ", fb(math.Log1p(v)), " ", fb(math.Cbrt(v)), " | ")
	}
	println()

	bases := []float64{nan, inf, -inf, 0, negz, 1, -1, 2, -2, 0.5, -0.5, 10, 1e-300, 1e300}
	exps := []float64{nan, inf, -inf, 0, negz, 1, -1, 2, 0.5, -0.5, 3, -3, 1e10, 1.5, 1023, -1074}
	reset()
	for _, b := range bases {
		for _, e := range exps {
			mix(math.Pow(b, e))
		}
	}
	println("pow-special", h, fb(math.Pow(-8, 1.0/3)), fb(math.Pow(2, -1074)), fb(math.Pow(2, 1024)), fb(math.Pow(-1, inf)))
	reset()
	for i := 1; i < 3000; i++ {
		b := float64(i) * 0.0731
		mix(math.Pow(b, float64(i%40)-20.5))
		mix(math.Pow(-b, float64(i%31)-15))
		mix(math.Hypot(b, 1/b))
		mix(math.Mod(b*1e3, 1.7))
		mix(math.Remainder(b*1e3, -1.7))
		fr, ex := math.Frexp(b * 1e-310)
		mix(fr)
		h = (h ^ uint64(ex)) * 1099511628211
		mix(math.Ldexp(b, i%2100-1050))
		ip, fp := math.Modf(-b * 3.3)
		mix(ip)
		mix(fp)
	}
	println("misc", h)
	println(fb(math.Nextafter(1, 2)), fb(math.Nextafter(0, -1)), fb(math.Nextafter(inf, 0)), math.Float32bits(math.Nextafter32(1, 0)))
	println(fb(math.FMA(0.1, 10, -1)), fb(math.FMA(1e308, 10, -inf)), fb(math.Mod(-0.0, 1)), fb(math.Remainder(5, 0)), fb(math.Hypot(inf, nan)))
	println(fb(math.Round(-2.5)), fb(math.RoundToEven(-2.5)), fb(math.Trunc(-0.7)), fb(math.Floor(-0.0)), fb(math.Ceil(-0.3)), fb(math.Dim(3, 5)), fb(math.Logb(1e-310)), math.Ilogb(0), math.Ilogb(1e-310))

	var bh uint64 = 14695981039346656037
	var v uint64 = 0x9e3779b97f4a7c15
	for i := 0; i < 500; i++ {
		v = v*0x5851f42d4c957f2d + 1
		hi, lo := bits.Mul64(v, v>>7)
		q, r := bits.Div64(hi%(v|1), lo, v|1)
		s, c := bits.Add64(v, hi, uint64(i&1))
		d, bo := bits.Sub64(lo, v, c)
		bh = (bh ^ hi ^ lo<<1 ^ q ^ r<<2 ^ s ^ d<<3 ^ bo) * 1099511628211
		bh = (bh ^ uint64(bits.LeadingZeros64(v>>uint(i%64))+bits.TrailingZeros32(uint32(v))<<8+bits.OnesCount16(uint16(v))<<16)) * 1099511628211
		bh = (bh ^ bits.RotateLeft64(v, i-250) ^ bits.Reverse64(v) ^ uint64(bits.ReverseBytes32(uint32(v))) ^ uint64(bits.Len8(uint8(v)))) * 1099511628211
	}
	println("bits", bh)

	reset()
	for i := -50; i <= 50; i++ {
		z := complex(float64(i)/7, float64(i*i%13)-6)
		for _, w := range []complex128{cmplx.Sqrt(z), cmplx.Exp(z), cmplx.Log(z), cmplx.Sin(z), cmplx.Tanh(z), cmplx.Pow(z, 1.5-0.5i), cmplx.Asin(z), cmplx.Atan(z)} {
			mix(real(w))
			mix(imag(w))
		}
		mix(cmplx.Abs(z))
		mix(cmplx.Phase(z))
	}
	println("cmplx", h, cmplx.IsNaN(cmplx.NaN()), cmplx.IsInf(cmplx.Inf()), fb(real(cmplx.Sqrt(-4))), fb(imag(cmplx.Sqrt(complex(-4, negz)))))
}
