package main

import (
	"fmt"
	"math"
	"math/cmplx"
)

func hex(z complex128) {
	fmt.Printf("%x %x\n", math.Float64bits(real(z)), math.Float64bits(imag(z)))
}

func main() {
	// Asin/Asinh/Atan/Atanh: real-axis fast paths, inf/nan matrix, and
	// the manualized complex-arithmetic tails.
	vs := []complex128{
		0, 1, -1, 0.5, 2, -2,
		complex(0, 1), complex(0, 2), complex(1, 1), complex(-2, 3),
		complex(math.Inf(1), 0), complex(0, math.Inf(1)),
		complex(math.Inf(1), math.Inf(1)), complex(math.NaN(), 0),
		complex(0, math.NaN()), complex(math.Inf(1), 1),
	}
	for _, z := range vs {
		hex(cmplx.Asin(z))
	}
	for _, z := range vs {
		hex(cmplx.Asinh(z))
	}
	for _, z := range vs {
		hex(cmplx.Atan(z))
	}
	for _, z := range vs {
		hex(cmplx.Atanh(z))
	}
	// Acos/Acosh: derived from Asin; zero and branch pins.
	for _, z := range []complex128{0, 1, -1, complex(1, 1), complex(math.Inf(1), 0)} {
		hex(cmplx.Acos(z))
		hex(cmplx.Acosh(z))
	}
	// Round-trips: Sin(Asin(z)) and Asin(Sin(z)) on parts.
	for _, z := range []complex128{complex(0.5, 0.25), complex(-0.3, 0.1)} {
		w := cmplx.Asin(z)
		s := cmplx.Sin(w)
		fmt.Println(math.Float64bits(real(s)) == math.Float64bits(real(z)) &&
			math.Float64bits(imag(s)) == math.Float64bits(imag(z)))
	}
}
