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
	// Sqrt: signed-zero preservation, negative real axis, inf, rescaling
	// paths (|a|>4 or |b|>4 vs tiny).
	for _, z := range []complex128{
		0, complex(0, 0), 1, -1, -4,
		complex(0, 1), complex(0, -1), complex(0, 2),
		complex(3, 4), complex(-3, 4), complex(3, -4), complex(-3, -4),
		complex(1e300, 1e300), complex(1e-300, 1e-300),
		complex(math.Inf(1), 0), complex(0, math.Inf(1)),
		complex(math.Inf(1), math.Inf(1)), complex(math.NaN(), 0),
		complex(-0.0, 0.0), complex(0.0, -0.0),
	} {
		hex(cmplx.Sqrt(z))
	}
	// Sqrt(z)^2 == z round-trip on parts (manualized, no complex arith).
	for _, z := range []complex128{complex(3, 4), complex(-3, 4), complex(1e100, -1e100)} {
		s := cmplx.Sqrt(z)
		sr, si := real(s), imag(s)
		rr := sr*sr - si*si
		ri := 2 * sr * si
		fmt.Println(math.Float64bits(rr) == math.Float64bits(real(z)) &&
			math.Float64bits(ri) == math.Float64bits(imag(z)))
	}
}
