package main

import (
	"fmt"
	"math"
)

func main() {
	// Floor/Ceil/Trunc/Round with sNaN (quieting check) and subnormals.
	snan := math.Float64frombits(0x7ff0000000000001)
	fmt.Printf("floor snan %x %x\n", math.Float64bits(snan), math.Float64bits(math.Floor(snan)))
	fmt.Printf("ceil snan %x %x\n", math.Float64bits(snan), math.Float64bits(math.Ceil(snan)))
	fmt.Printf("trunc snan %x %x\n", math.Float64bits(snan), math.Float64bits(math.Trunc(snan)))
	for _, v := range []float64{2.5, -2.5, 2.4, -2.4, 0.5, -0.5, math.SmallestNonzeroFloat64, 1e300} {
		fmt.Printf("floor %x %.17g\n", math.Float64bits(v), math.Floor(v))
		fmt.Printf("ceil %x %.17g\n", math.Float64bits(v), math.Ceil(v))
		fmt.Printf("trunc %x %.17g\n", math.Float64bits(v), math.Trunc(v))
		fmt.Printf("round %x %.17g\n", math.Float64bits(v), math.Round(v))
		fmt.Printf("roundtoeven %x %.17g\n", math.Float64bits(v), math.RoundToEven(v))
	}
	// Abs, Signbit, IsNaN, IsInf.
	fmt.Printf("%x\n", math.Float64bits(math.Abs(-2.5)))
	fmt.Printf("%v %v %v\n", math.Signbit(-1.0), math.Signbit(1.0), math.Signbit(math.NaN()))
	fmt.Printf("%v %v\n", math.IsNaN(math.NaN()), math.IsNaN(1.0))
	fmt.Printf("%v %v %v\n", math.IsInf(math.Inf(1), 1), math.IsInf(math.Inf(-1), -1), math.IsInf(1.0, 0))
	// Constants.
	fmt.Printf("%.17g %.17g %.17g\n", math.Pi, math.E, math.Phi)
	fmt.Printf("%.17g %.17g\n", math.Sqrt2, math.Ln2)
	fmt.Printf("%d %d\n", math.MaxInt64, math.MinInt64)
}
