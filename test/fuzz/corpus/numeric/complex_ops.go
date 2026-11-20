// complex64/complex128 arithmetic: + - * /, division by zero and by
// Inf/NaN components (Go's runtime complex division rules), real/imag/
// complex builtins, equality with NaN and -0 parts, complex64 per-op
// rounding, and println formatting of complex values.
package main

import "math"

var inf = math.Inf(1)
var nan = math.NaN()
var nz = math.Copysign(0, -1)

var cs = []complex128{0, 1, -1, 1i, -1i, complex(1, 1), complex(3, -4), complex(1e308, 1e308), complex(1e-308, 2e-308),
	complex(inf, 0), complex(0, inf), complex(inf, inf), complex(nan, 0), complex(0, nan), complex(nz, nz), complex(0.1, 0.2),
	complex(5e-324, 0), complex(-2.5, 1e10)}

func div(a, b complex128) complex128 { return a / b }
func div64(a, b complex64) complex64 { return a / b }

func p(tag string, c complex128) {
	// print NaN parts without a sign-dependent representation
	println(tag, c, real(c), imag(c))
}

func main() {
	for _, a := range cs {
		for _, b := range cs {
			println("c", a, b, a+b, a-b, a*b, div(a, b), a == b, a != b)
		}
	}
	for _, a := range cs {
		x := complex64(a)
		for _, b := range cs[:8] {
			y := complex64(b)
			println("c64", x, y, x+y, x-y, x*y, div64(x, y))
		}
	}
	// division by zero does not panic
	var z complex128
	var z64 complex64
	p("1/0", div(1, z))
	p("i/0", div(1i, z))
	p("0/0", div(z, z))
	p("(1+1i)/0", div(complex(1, 1), z))
	p("-1/0", div(-1, z))
	println("c64/0", div64(1, z64), div64(complex(1, -1), z64), div64(0, z64))
	// real/imag/complex with typed and untyped
	for _, c := range cs {
		r, i := real(c), imag(c)
		back := complex(r, i)
		println("ri", r, i, back == c, -c, complex(i, r), real(complex64(c)), imag(complex64(c)))
	}
	// negative zero parts
	c0 := complex(nz, 0)
	println("nz", c0, -c0, c0 == 0, math.Signbit(real(c0)), math.Signbit(imag(-c0)), math.Signbit(real(-c0)))
	// powers by repeated multiplication
	w := complex(0.6, 0.8)
	acc := complex128(1)
	for i := 0; i < 25; i++ {
		acc *= w
		println("pow", i, acc)
	}
	acc64 := complex64(1)
	w64 := complex64(w)
	for i := 0; i < 25; i++ {
		acc64 *= w64
		println("pow64", i, acc64, real(acc64)*real(acc64)+imag(acc64)*imag(acc64))
	}
	// compound ops
	q := complex(2, 3)
	q += 1i
	q -= complex(0.5, 0)
	q *= q
	q /= complex(0, 2)
	println("compound", q)
	// complex constants
	const k = 1 + 2i
	const kk = k * k * k
	println("const", k, kk, real(kk), imag(kk), complex64(k)/3, k/3)
	var ck complex64 = 1e38 + 1e38i
	println("ovf64", ck*ck, ck+ck, ck*10)
	// large magnitude division that needs scaling
	a := complex(1e300, 1e300)
	b := complex(1e300, -1e300)
	println("scale", div(a, b), div(1, complex(1e-300, 1e-300)), div(complex(1e-310, 0), complex(1e-310, 1e-310)))
	// equality loops
	eq := 0
	for _, a := range cs {
		for _, b := range cs {
			if a == b {
				eq++
			}
		}
	}
	println("eq", eq)
}
