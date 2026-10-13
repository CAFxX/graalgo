package main

import (
	"fmt"
	"math"
)

func main() {
	// Binary functions: Pow, Atan2, Hypot, Mod, Remainder, Max, Min,
	// Dim, Copysign.
	pairs := [][2]float64{
		{2, 3}, {-2, 3}, {2, -3}, {0.5, 2}, {10, 0.5},
		{math.Inf(1), 2}, {2, math.Inf(1)}, {math.NaN(), 2},
		{1e300, 2}, {1e-300, 2}, {0, 0}, {-0.0, 1},
		{3, 4}, {5, 12}, {1e300, 1e300},
	}
	for _, p := range pairs {
		x, y := p[0], p[1]
		fmt.Printf("pow %x %x %x\n", math.Float64bits(x), math.Float64bits(y), math.Float64bits(math.Pow(x, y)))
		fmt.Printf("atan2 %x %x %.17g\n", math.Float64bits(x), math.Float64bits(y), math.Atan2(x, y))
		fmt.Printf("hypot %x %x %.17g\n", math.Float64bits(x), math.Float64bits(y), math.Hypot(x, y))
		fmt.Printf("mod %x %x %.17g\n", math.Float64bits(x), math.Float64bits(y), math.Mod(x, y))
		fmt.Printf("rem %x %x %.17g\n", math.Float64bits(x), math.Float64bits(y), math.Remainder(x, y))
		fmt.Printf("max %x %x %x\n", math.Float64bits(x), math.Float64bits(y), math.Float64bits(math.Max(x, y)))
		fmt.Printf("min %x %x %x\n", math.Float64bits(x), math.Float64bits(y), math.Float64bits(math.Min(x, y)))
		fmt.Printf("dim %x %x %.17g\n", math.Float64bits(x), math.Float64bits(y), math.Dim(x, y))
		fmt.Printf("copysign %x %x %x\n", math.Float64bits(x), math.Float64bits(y), math.Float64bits(math.Copysign(x, y)))
	}
	// Ldexp with extreme exponents.
	for _, e := range []int{-1075, -1023, -52, 0, 52, 1023, 1074} {
		fmt.Printf("ldexp %x %d %x\n", math.Float64bits(1.5), e, math.Float64bits(math.Ldexp(1.5, e)))
	}
	// Nextafter.
	fmt.Printf("%x\n", math.Float64bits(math.Nextafter(0, 1)))
	fmt.Printf("%x\n", math.Float64bits(math.Nextafter(0, -1)))
	fmt.Printf("%x\n", math.Float64bits(math.Nextafter(1, 2)))
}
