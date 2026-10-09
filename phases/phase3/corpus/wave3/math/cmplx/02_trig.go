package main

import (
	"fmt"
	"math"
	"math/cmplx"
)

func hex(z complex128) {
	// P3-M9: normalize NaN payloads (frontend may canonicalize in some
	// contexts; the payload is implementation-defined). Check IsNaN
	// separately where payload matters.
	re, im := real(z), imag(z)
	if math.IsNaN(re) {
		re = math.NaN()
	}
	if math.IsNaN(im) {
		im = math.NaN()
	}
	fmt.Printf("%x %x\n", math.Float64bits(re), math.Float64bits(im))
}

func main() {
	// Sin/Cos: real-axis special values + general complex + inf/nan matrix.
	// (P3-M9: avoid []complex128{0, 1, ...} — frontend can't convert
	// UNTYPED_FLOAT to COMPLEX128 in composite literals, 2026-10-09.)
	sinCosVals := []complex128{
		complex(0, 0), complex(1, 0), complex(-1, 0), complex(math.Pi/2, 0), complex(math.Pi, 0),
		complex(1, 1), complex(-2, 3), complex(0.5, -0.25),
		complex(math.Inf(1), 0), complex(0, math.Inf(1)),
		complex(math.Inf(1), math.Inf(1)), complex(math.NaN(), 0),
		complex(0, math.NaN()), complex(math.Inf(1), 1),
	}
	for _, z := range sinCosVals {
		hex(cmplx.Sin(z))
		hex(cmplx.Cos(z))
	}
	// Sinh/Cosh.
	sinhCoshVals := []complex128{complex(0, 0), complex(1, 0), complex(1, 1), complex(math.Inf(1), 0), complex(0, math.Inf(1))}
	for _, z := range sinhCoshVals {
		hex(cmplx.Sinh(z))
		hex(cmplx.Cosh(z))
	}
	// Tan/Tanh: incl. the tanSeries path (small |d|) and poles.
	tanVals := []complex128{
		complex(0, 0), complex(1, 0), complex(1, 1), complex(0.1, 0.2),
		complex(math.Inf(1), 0), complex(0, math.Inf(1)),
		complex(math.Inf(1), math.Inf(1)), complex(0, math.NaN()),
	}
	for _, z := range tanVals {
		hex(cmplx.Tan(z))
		hex(cmplx.Tanh(z))
	}
	// Identity: Sin^2 + Cos^2 == 1 (manualized: no complex arithmetic in
	// the corpus either, so the identity is checked on parts).
	for _, z := range []complex128{complex(1, 1), complex(-2, 3), complex(0.5, -0.25)} {
		s, c := cmplx.Sin(z), cmplx.Cos(z)
		sr, si := real(s), imag(s)
		cr, ci := real(c), imag(c)
		// (sr+i*si)^2 + (cr+i*ci)^2, real and imag parts.
		rr := sr*sr - si*si + cr*cr - ci*ci
		ri := 2*sr*si + 2*cr*ci
		fmt.Printf("%.17g %.17g\n", rr, ri)
	}
}
