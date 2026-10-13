package main

import (
	"fmt"
	"math"
)

func main() {
	// Bit-conversion intrinsics: print as hex (bit-exact).
	fmt.Printf("%x\n", math.Float64bits(1.5))
	fmt.Printf("%x\n", math.Float64bits(-0.0))
	fmt.Printf("%x\n", math.Float64bits(math.Inf(1)))
	fmt.Printf("%x\n", math.Float64bits(math.NaN()))
	fmt.Printf("%.17g\n", math.Float64frombits(0x3ff8000000000000))
	fmt.Printf("%.17g\n", math.Float64frombits(0x7ff8000000000001))
	fmt.Printf("%x\n", math.Float32bits(1.5))
	fmt.Printf("%x\n", math.Float32bits(-0.0))
	fmt.Printf("%.17g\n", math.Float32frombits(0x3fc00000))
	// Sqrt / FMA intrinsics: %.17g catches bit differences.
	fmt.Printf("%.17g\n", math.Sqrt(2.0))
	fmt.Printf("%.17g\n", math.Sqrt(-1.0))
	fmt.Printf("%x\n", math.Float64bits(math.Sqrt(-1.0)))
	fmt.Printf("%.17g\n", math.FMA(1.5, 2.5, 3.5))
	fmt.Printf("%.17g\n", math.FMA(math.Inf(1), 0, 1))
	fmt.Printf("%.17g\n", math.FMA(1e300, 1e300, -1e300))
}
