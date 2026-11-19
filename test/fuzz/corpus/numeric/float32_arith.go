// float32 arithmetic must round to float32 after every operation (no
// float64 intermediate leakage): accumulations, products, division,
// mixed-constant expressions, compound assignment, and comparisons whose
// result flips if extra precision is kept. Bits are printed alongside.
package main

import "math"

var xs = []float32{0.1, 0.2, 0.3, 1.0 / 3, 16777216, 16777217, 1e-7, 3.4e38, 1.17549435e-38, 1.4e-45, 7, 1e10, 0.7, 1.1}

func add(a, b float32) float32 { return a + b }
func mul(a, b float32) float32 { return a * b }
func div(a, b float32) float32 { return a / b }

func main() {
	for _, a := range xs {
		for _, b := range xs {
			s, m, d := add(a, b), mul(a, b), div(a, b)
			println("op", a, b, s, a-b, m, d, math.Float32bits(s), math.Float32bits(m), math.Float32bits(d))
		}
	}
	// accumulation: float32 vs float64
	var s32 float32
	var s64 float64
	for i := 0; i < 10000; i++ {
		s32 += 0.1
		s64 += 0.1
	}
	println("acc", s32, s64, math.Float32bits(s32), float32(s64))
	// a + b - a must be computed in float32
	big := xs[4]
	one := float32(1)
	println("absorb", big+one-big, (big+one)-big == 0, big+one == big, float32(float64(big)+1)-big)
	// 0.1+0.2 in float32 vs float64
	a, b := xs[0], xs[1]
	println("pt3", a+b, float64(a+b), a+b == 0.3, float64(a)+float64(b), float32(float64(a)+float64(b)) == a+b)
	// a*b/b roundtrip
	cnt := 0
	for i := 1; i < 2000; i++ {
		x := float32(i) * 0.37
		y := float32(i%97+1) * 1.3
		if (x*y)/y != x {
			cnt++
		}
	}
	println("roundtrip mismatches", cnt)
	// harmonic sum in float32 stops growing
	var h float32
	n := 0
	for i := 1; i < 3000000; i++ {
		prev := h
		h += 1 / float32(i)
		if h == prev {
			n = i
			break
		}
	}
	println("harmonic", h, n, math.Float32bits(h))
	// overflow and underflow in float32 ops
	m := xs[7]
	tiny := xs[9]
	println("ovf", m*10, m+m, -m*10, m*10-m*10)
	println("unf", tiny/2, tiny*0.5, tiny/3*3, tiny*1.5, xs[8]/3, math.Float32bits(xs[8]/3), xs[8]*xs[8])
	// compound assignment and increments on float32
	f := float32(0.5)
	for i := 0; i < 30; i++ {
		f *= 1.7
		f -= 0.3
		f /= 1.1
		f++
		println("cmp", i, f, math.Float32bits(f))
	}
	// powers by repeated multiply
	p := float32(1)
	for i := 0; i < 140; i += 7 {
		p *= 1e-2
		println("pow", i, p)
	}
	// series computed in float32: sin(x) Taylor
	for _, x := range []float32{0.5, 1, 2, 3.14159265} {
		term, sum := x, x
		for k := 1; k < 12; k++ {
			term = term * (-x * x) / float32((2*k)*(2*k+1))
			sum += term
		}
		println("sin", x, sum, math.Float32bits(sum))
	}
	// constants with float32 type are rounded at compile time
	const c = 0.1
	var cf float32 = c
	println("const", cf, float32(c)*3, cf*3, float32(c*3), cf == float32(c), float64(cf) == c)
	// float32 negation and abs
	for _, v := range xs[:6] {
		println("neg", -v, float32(math.Abs(float64(-v))), math.Float32bits(-v))
	}
	// float32 from int arithmetic
	for i := int32(16777210); i < 16777225; i++ {
		f := float32(i)
		println("i2f", i, f, f+1, int32(f+1), f*2)
	}
	// sqrt through float64 and back
	for _, v := range xs[:8] {
		r := float32(math.Sqrt(float64(v)))
		println("sqrt", v, r, r*r, r*r == v)
	}
}
