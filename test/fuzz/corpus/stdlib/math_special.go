// Targets bit-exact results of math special functions over sweeps:
// Gamma, Lgamma (with sign), Erf/Erfc/Erfinv/Erfcinv, J0/J1/Jn/Y0/Y1/Yn,
// plus their special-value cases (NaN, +-Inf, +-0, poles, negative
// integers); everything is printed via math.Float64bits.
package main

import "math"

var h uint64

func reset() { h = 14695981039346656037 }

func mix(f float64) {
	b := math.Float64bits(f)
	if f != f {
		b = 0x7ff8000000000001 // canonicalize NaN payloads
	}
	h = (h ^ b) * 1099511628211
}

func bits(f float64) uint64 {
	if f != f {
		return 1
	}
	return math.Float64bits(f)
}

func main() {
	nan := math.NaN()
	inf := math.Inf(1)
	negz := math.Copysign(0, -1)
	specials := []float64{nan, inf, -inf, 0, negz, 1, -1, 2, -2, 0.5, -0.5, 1e-300, -1e-300, 171.6, 171.7, -170.5, -171.5, 1e10, -1e10, 5e-324}
	for _, x := range specials {
		lg, s := math.Lgamma(x)
		println(bits(x), bits(math.Gamma(x)), bits(lg), s, bits(math.Erf(x)), bits(math.Erfc(x)))
		println(" ", bits(math.J0(x)), bits(math.J1(x)), bits(math.Y0(x)), bits(math.Y1(x)), bits(math.Jn(3, x)), bits(math.Yn(2, x)), bits(math.Jn(-2, x)), bits(math.Yn(-3, x)))
	}
	for _, x := range []float64{nan, -1, 1, 0, negz, 0.5, -0.999999, 0.999999999, 1.0000001, -inf, 2} {
		println(bits(x), bits(math.Erfinv(x)), bits(math.Erfcinv(x)))
	}

	reset()
	for i := -2000; i <= 2000; i++ {
		x := float64(i) / 11.3
		mix(math.Gamma(x))
		lg, s := math.Lgamma(x)
		mix(lg)
		h = (h ^ uint64(s+2)) * 1099511628211
	}
	println("gamma", h)
	for _, x := range []float64{0.1, 1.5, 3.7, 10.25, 20.5, 33.3, 100.1, 170.9, -0.1, -1.5, -2.5, -33.3, -100.5, -169.5} {
		lg, s := math.Lgamma(x)
		print(bits(math.Gamma(x)), "/", bits(lg), "/", s, " ")
	}
	println()

	reset()
	for i := -3000; i <= 3000; i++ {
		x := float64(i) / 500
		mix(math.Erf(x))
		mix(math.Erfc(x))
	}
	println("erf", h)
	reset()
	for i := -999; i <= 999; i++ {
		y := float64(i) / 1000
		mix(math.Erfinv(y))
		mix(math.Erfcinv(1 + y))
	}
	println("erfinv", h, bits(math.Erfinv(0.5)), bits(math.Erfinv(-0.9999)), bits(math.Erfcinv(1e-10)))

	reset()
	for i := 1; i <= 3000; i++ {
		x := float64(i) * 0.037
		mix(math.J0(x))
		mix(math.J1(x))
		mix(math.Y0(x))
		mix(math.Y1(x))
		mix(math.J0(-x))
		mix(math.J1(-x))
	}
	println("bessel01", h)
	reset()
	for n := -5; n <= 30; n += 3 {
		for i := 0; i <= 200; i++ {
			x := float64(i)*0.73 + 0.01
			mix(math.Jn(n, x))
			mix(math.Yn(n, x))
			mix(math.Jn(n, -x))
		}
	}
	println("besseln", h)
	for _, x := range []float64{0.001, 1, 2.4048255576957727, 5, 25, 100, 1e5, 1e20} {
		print(bits(math.J0(x)), " ", bits(math.Y0(x)), " ", bits(math.Jn(5, x)), " ", bits(math.Yn(7, x)), " | ")
	}
	println()
	// large-argument asymptotic branches
	reset()
	x := 1.0
	for i := 0; i < 600; i++ {
		x *= 1.17
		mix(math.J0(x))
		mix(math.Y1(x))
		mix(math.Erf(1 / x))
		mix(math.Gamma(1 + 1/x))
	}
	println("asym", h)
}
