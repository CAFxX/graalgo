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
	// Exp: special-value matrix from the gc source's switch.
	for _, z := range []complex128{
		0, 1, -1,
		complex(math.Inf(1), 0), complex(math.Inf(-1), 0),
		complex(math.Inf(1), 1), complex(math.Inf(1), math.Inf(1)),
		complex(math.Inf(-1), math.Inf(1)), complex(math.Inf(1), math.NaN()),
		complex(math.NaN(), 0), complex(math.NaN(), 1),
		complex(1, 1), complex(-2, 3), complex(0, math.Pi),
	} {
		hex(cmplx.Exp(z))
	}
	// Log / Log10: branch cut on the negative real axis, zeros, inf.
	for _, z := range []complex128{
		1, -1, 0, complex(0, 1), complex(3, 4),
		complex(math.Inf(1), 0), complex(-2, 0), complex(-2, 1e-300),
	} {
		hex(cmplx.Log(z))
		hex(cmplx.Log10(z))
	}
	// Pow: x == 0 matrix, y NaN, general values.
	for _, y := range []complex128{0, 1, -1, complex(0, 1), complex(math.NaN(), 0)} {
		hex(cmplx.Pow(0, y))
	}
	hex(cmplx.Pow(complex(2, 0), complex(3, 0)))
	hex(cmplx.Pow(complex(1, 1), complex(2, 0)))
	hex(cmplx.Pow(complex(2, 3), complex(1, -1)))
	hex(cmplx.Pow(complex(-1, 0), complex(0, 1)))
	// Exp(Log(z)) round-trip on non-cut values.
	for _, z := range []complex128{complex(3, 4), complex(1, -2), complex(0.5, 0.25)} {
		w := cmplx.Exp(cmplx.Log(z))
		fmt.Println(math.Float64bits(real(w)) == math.Float64bits(real(z)) &&
			math.Float64bits(imag(w)) == math.Float64bits(imag(z)))
	}
}
