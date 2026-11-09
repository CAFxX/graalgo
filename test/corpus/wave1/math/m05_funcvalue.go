package main

import (
	"fmt"
	"math"
)

// R10-09: function values of math intrinsics. The value form must behave
// exactly like the direct call (m00_bits.go covers the direct calls):
// f := math.Sqrt; f(2) is 1.414... under gc, not a compile-time scope
// error and not a runtime panic. Same for the bit conversions and FMA,
// and for an intrinsic passed as an argument.
func apply(f func(float64) float64, x float64) float64 { return f(x) }

func main() {
	// Sqrt as a value, including a negative argument (NaN).
	f := math.Sqrt
	fmt.Printf("%.17g\n", f(2.0))
	fmt.Printf("%.17g\n", f(-1.0))
	fmt.Printf("%x\n", math.Float64bits(f(-1.0)))
	// Bit conversions as values, both directions and both widths.
	g := math.Float64bits
	fmt.Printf("%x\n", g(1.5))
	fmt.Printf("%x\n", g(-0.0))
	fmt.Printf("%x\n", g(math.Inf(1)))
	u64 := math.Float64frombits
	fmt.Printf("%.17g\n", u64(0x3ff8000000000000))
	fmt.Printf("%.17g\n", u64(0x7ff8000000000001))
	b32 := math.Float32bits
	fmt.Printf("%x\n", b32(1.5))
	fmt.Printf("%x\n", b32(-0.0))
	u32 := math.Float32frombits
	fmt.Printf("%.17g\n", u32(0x3fc00000))
	// FMA as a value (3 args).
	h := math.FMA
	fmt.Printf("%.17g\n", h(1.5, 2.5, 3.5))
	fmt.Printf("%.17g\n", h(math.Inf(1), 0, 1))
	fmt.Printf("%.17g\n", h(1e300, 1e300, -1e300))
	// Intrinsic passed as an argument.
	fmt.Printf("%.17g\n", apply(math.Sqrt, 2))
}
