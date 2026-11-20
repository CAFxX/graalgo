// math/cmplx over special and boundary inputs: Abs, Phase, Polar/Rect,
// Sqrt (branch cuts with ±0 imaginary parts), Exp, Log, Pow, Conj, Inf,
// NaN, IsInf/IsNaN, trig — printed via strconv bit patterns so ±0 and
// last-bit differences are visible.
package main

import (
	"math"
	"math/cmplx"
	"strconv"
)

var nz = math.Copysign(0, -1)
var inf = math.Inf(1)
var nan = math.NaN()

func fb(x float64) string {
	if x != x {
		return "NaN"
	}
	return strconv.FormatFloat(x, 'g', -1, 64) + "/" + strconv.FormatUint(math.Float64bits(x)>>63, 16)
}

func cb(c complex128) string { return "(" + fb(real(c)) + "," + fb(imag(c)) + ")" }

var zs = []complex128{0, complex(nz, 0), complex(0, nz), complex(nz, nz), 1, -1, 1i, -1i, complex(-1, nz), complex(-4, 0), complex(-4, nz),
	complex(3, 4), complex(-3, -4), complex(0.5, -0.5), complex(1e300, 1e300), complex(1e-310, 1e-310), complex(inf, 0), complex(-inf, 0),
	complex(0, inf), complex(inf, nan), complex(nan, inf), complex(nan, 0), complex(-inf, 1), complex(inf, -1), complex(math.Pi, math.E),
	complex(710, 0), complex(0, 710), complex(-1e-20, 1)}

func main() {
	for _, z := range zs {
		println("z", cb(z), fb(cmplx.Abs(z)), fb(cmplx.Phase(z)), cmplx.IsInf(z), cmplx.IsNaN(z))
		r, th := cmplx.Polar(z)
		println(" polar", fb(r), fb(th), cb(cmplx.Rect(r, th)), cb(cmplx.Conj(z)))
		println(" sqrt", cb(cmplx.Sqrt(z)))
		println(" exp", cb(cmplx.Exp(z)))
		println(" log", cb(cmplx.Log(z)), cb(cmplx.Log10(z)))
		println(" trig", cb(cmplx.Sin(z)), cb(cmplx.Cos(z)), cb(cmplx.Tan(z)))
		println(" hyp", cb(cmplx.Sinh(z)), cb(cmplx.Cosh(z)), cb(cmplx.Tanh(z)))
		println(" inv", cb(cmplx.Asin(z)), cb(cmplx.Acos(z)), cb(cmplx.Atan(z)))
	}
	ps := []complex128{0, 1, -1, 2, 0.5, 1i, complex(1, 1), complex(-2, 0), complex(0, nz), complex(inf, 0), complex(nan, 0)}
	for _, x := range ps {
		for _, y := range ps {
			println("pow", cb(x), cb(y), cb(cmplx.Pow(x, y)))
		}
	}
	println("inf", cb(cmplx.Inf()), "nan", cmplx.IsNaN(cmplx.NaN()), cmplx.IsInf(cmplx.NaN()))
	// complex64 through cmplx
	for _, z := range zs[:15] {
		c := complex64(z)
		s := complex64(cmplx.Sqrt(complex128(c)))
		println("c64", c, s, s*s)
	}
	// roots of unity
	for n := 1; n <= 12; n++ {
		sum := complex128(0)
		for k := 0; k < n; k++ {
			sum += cmplx.Exp(complex(0, 2*math.Pi*float64(k)/float64(n)))
		}
		println("unity", n, cmplx.Abs(sum) < 1e-14, cb(cmplx.Pow(cmplx.Exp(complex(0, 2*math.Pi/float64(n))), complex(float64(n), 0))))
	}
	println("abs big", fb(cmplx.Abs(complex(math.MaxFloat64, math.MaxFloat64))), fb(cmplx.Abs(complex(3e-320, 4e-320))))
}
