package main

import (
	"fmt"
	"math"
	"math/cmplx"
)

// hex prints a complex128 as the hex bit patterns of its real and
// imaginary parts (the strongest differential pin).
func hex(z complex128) {
	fmt.Printf("%x %x\n", math.Float64bits(real(z)), math.Float64bits(imag(z)))
}

func main() {
	// Abs: incl. zeros, infinities, NaN, overflow-safe Hypot path.
	for _, z := range []complex128{0, 1, complex(3, 4), complex(-3, -4), complex(1e300, 1e300), complex(math.Inf(1), 1), complex(1, math.Inf(-1)), complex(math.NaN(), 0)} {
		fmt.Printf("%.17g\n", cmplx.Abs(z))
	}
	// Phase: quadrants, axes, signed zeros, infinities.
	for _, z := range []complex128{0, 1, -1, complex(0, 1), complex(0, -1), complex(1, 1), complex(-1, 1), complex(-1, -1), complex(1, -1), complex(math.Inf(1), math.Inf(1)), complex(math.Inf(-1), 0)} {
		fmt.Printf("%.17g\n", cmplx.Phase(z))
	}
	// Polar: 2-target Appendix M desugar; Rect(Polar(z)) round-trip.
	for _, z := range []complex128{complex(3, 4), complex(-1, 0), complex(0, 2), 0} {
		r, theta := cmplx.Polar(z)
		fmt.Printf("%.17g %.17g\n", r, theta)
		hex(cmplx.Rect(r, theta))
	}
	// Rect: direct pins incl. r=0 and negative r.
	hex(cmplx.Rect(0, 1))
	hex(cmplx.Rect(2, math.Pi/2))
	hex(cmplx.Rect(-1, 0))
	// Conj.
	hex(cmplx.Conj(complex(3, 4)))
	hex(cmplx.Conj(complex(3, -4)))
	hex(cmplx.Conj(0))
}
