package main

import (
	"fmt"
	"math"
)

func main() {
	// Bessel functions, Gamma, Erf — adversarial values.
	for _, v := range []float64{0, 0.5, 1, 2, 5, 10, 20, -1, math.Inf(1), math.NaN()} {
		fmt.Printf("j0 %x %.17g\n", math.Float64bits(v), math.J0(v))
		fmt.Printf("j1 %x %.17g\n", math.Float64bits(v), math.J1(v))
		fmt.Printf("y0 %x %.17g\n", math.Float64bits(v), math.Y0(v))
		fmt.Printf("y1 %x %.17g\n", math.Float64bits(v), math.Y1(v))
		fmt.Printf("gamma %x %.17g\n", math.Float64bits(v), math.Gamma(v))
		fmt.Printf("erf %x %.17g\n", math.Float64bits(v), math.Erf(v))
		fmt.Printf("erfc %x %.17g\n", math.Float64bits(v), math.Erfc(v))
	}
	for _, n := range []int{0, 1, 2, 5, 10} {
		fmt.Printf("jn %d %.17g %.17g\n", n, 2.5, math.Jn(n, 2.5))
		fmt.Printf("yn %d %.17g %.17g\n", n, 2.5, math.Yn(n, 2.5))
	}
	fmt.Printf("erfinv %.17g %.17g\n", 0.5, math.Erfinv(0.5))
	fmt.Printf("erfcinv %.17g %.17g\n", 0.5, math.Erfcinv(0.5))
}
