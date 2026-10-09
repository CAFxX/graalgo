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
	// IsInf / IsNaN truth tables.
	for _, z := range []complex128{
		0, 1, complex(1, 1),
		complex(math.Inf(1), 0), complex(0, math.Inf(-1)),
		complex(math.Inf(1), math.Inf(1)),
		complex(math.NaN(), 0), complex(0, math.NaN()),
		complex(math.NaN(), math.NaN()),
		complex(math.Inf(1), math.NaN()),
	} {
		fmt.Println(cmplx.IsInf(z), cmplx.IsNaN(z))
	}
	// Inf() / NaN() constructors.
	hex(cmplx.Inf())
	hex(cmplx.NaN())
	fmt.Println(cmplx.IsInf(cmplx.Inf()), cmplx.IsNaN(cmplx.NaN()))
	// Cot: 1/Tan via parts (no complex division in the corpus).
	for _, z := range []complex128{complex(1, 1), complex(0.5, -0.25)} {
		t := cmplx.Tan(z)
		tr, ti := real(t), imag(t)
		den := tr*tr + ti*ti
		fmt.Printf("%.17g %.17g\n", tr/den, -ti/den)
	}
}
