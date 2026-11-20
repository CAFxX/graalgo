// complex128 and complex64 arithmetic corners: division by zero, Inf and
// NaN parts (gc's complex128div rules), complex64 / complex64 computed in
// float64 then rounded, negation of zeros, comparisons and cmplx helpers.
package main

import (
	"math"
	"math/cmplx"
	"strconv"
)

// sign reports the sign bit, except for NaN whose sign is unspecified.
func sign(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	return strconv.FormatBool(math.Signbit(f))
}

func bits32(f float32) string {
	if f != f {
		return "nan"
	}
	return strconv.FormatUint(uint64(math.Float32bits(f)), 10)
}

func show(name string, c complex128) {
	println(name, strconv.FormatComplex(c, 'g', -1, 128), sign(real(c)), sign(imag(c)))
}

func show64(name string, c complex64) {
	println(name, strconv.FormatComplex(complex128(c), 'g', -1, 64), bits32(real(c)), bits32(imag(c)))
}

//go:noinline
func div(a, b complex128) complex128 { return a / b }

//go:noinline
func div64(a, b complex64) complex64 { return a / b }

//go:noinline
func mul64(a, b complex64) complex64 { return a * b }

func main() {
	inf := math.Inf(1)
	nan := math.NaN()
	negz := math.Copysign(0, -1)
	zero := complex(0, 0)

	show("1/0", div(1, zero))
	show("(1+1i)/0", div(complex(1, 1), zero))
	show("0/0", div(zero, zero))
	show("-1/0", div(-1, zero))
	show("1/-0", div(1, complex(negz, negz)))
	show("inf/1", div(complex(inf, 0), 1))
	show("1/inf", div(1, complex(inf, 0)))
	show("1/(inf+inf i)", div(1, complex(inf, inf)))
	show("(inf+nan i)/2", div(complex(inf, nan), 2))
	show("(nan+inf i)/(1+1i)", div(complex(nan, inf), complex(1, 1)))
	show("(1+2i)/(nan+0i)", div(complex(1, 2), complex(nan, 0)))
	show("(inf+inf i)/(inf+inf i)", div(complex(inf, inf), complex(inf, inf)))
	show("big/big", div(complex(1e300, 1e300), complex(1e300, 1e-300)))
	show("tiny/tiny", div(complex(1e-310, 3e-310), complex(2e-310, 1e-310)))
	show("(3+4i)/(1-2i)", div(complex(3, 4), complex(1, -2)))

	show("inf*0", complex(inf, 0)*complex(0, 1))
	show("(inf+inf i)*(1+0i)", complex(inf, inf)*complex(1, 0))
	show("neg zero", -complex(0, 0))
	show("neg negz", -complex(negz, negz))
	show("zero-zero", complex(0, 0)-complex(0, 0))
	show("negz+negz", complex(negz, negz)+complex(negz, negz))
	show("negz*1", complex(negz, 0)*complex(1, 0))

	show64("c64 1/0", div64(1, 0))
	show64("c64 0/0", div64(0, 0))
	show64("c64 (1+2i)/(3+4i)", div64(complex(1, 2), complex(3, 4)))
	show64("c64 big/small", div64(complex(3e38, 3e38), complex(1e-30, 1e-30)))
	show64("c64 small/big", div64(complex(1e-38, 1e-38), complex(3e38, 1e30)))
	show64("c64 rounding", div64(complex(0.1, 0.7), complex(0.3, 1.9)))
	show64("c64 mul overflow", mul64(complex(3e38, 3e38), complex(3e38, -3e38)))
	show64("c64 mul round", mul64(complex(16777217.0/3, 1.0/3), complex(1.0/7, 16777215.0/11)))

	var acc complex64 = complex(1, 1)
	for i := 0; i < 20; i++ {
		acc = acc * complex(float32(i)+0.5, 0.25)
		acc /= complex(float32(i)+0.75, -0.125)
	}
	show64("c64 loop", acc)
	var acc2 complex128 = complex(1, 1)
	for i := 0; i < 20; i++ {
		acc2 = acc2 * complex(float64(i)+0.5, 0.25)
		acc2 /= complex(float64(i)+0.75, -0.125)
	}
	show("c128 loop", acc2)

	println("eq nan", complex(nan, 0) == complex(nan, 0), complex(nan, 0) != complex(nan, 0))
	println("eq negz", complex(negz, 0) == complex(0, negz))
	var x any = complex64(complex(1, 2))
	var y any = complex(1, 2)
	println("iface eq c64 vs c128", x == y, x == any(complex64(1+2i)))
	println("cmplx", cmplx.IsInf(complex(inf, nan)), cmplx.IsNaN(complex(inf, nan)), cmplx.IsNaN(complex(1, nan)))
	show("cmplx.Inf", cmplx.Inf())
	show("cmplx.NaN", cmplx.NaN())
	show("abs", complex(cmplx.Abs(complex(3e200, 4e200)), cmplx.Abs(complex(inf, nan))))
	show("sqrt -4", cmplx.Sqrt(-4))
	show("sqrt -4-0i", cmplx.Sqrt(complex(-4, negz)))
	show("conj", cmplx.Conj(complex(1, 0)))
	println("println", complex(1.5, -2.25), complex64(complex(negz, inf)))
	println("real/imag c64", real(complex64(complex(0.1, 0.2))), imag(complex64(complex(0.1, 0.2))))
}
