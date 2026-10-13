package main

import (
	"fmt"
	"math"
)

func main() {
	// Transcendentals with adversarial values. %.17g and %x catch
	// single-bit differences.
	vals := []float64{
		0, 1, -1, 0.5, -0.5, 2, -2, 10, -10,
		math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64,
		math.MaxFloat64, -math.MaxFloat64,
		1e-300, -1e-300, 1e300, -1e300,
		710, 709.78, 709.79, -745, -1000,
		math.Pi, math.E, 1.5, 2.5,
	}
	for _, v := range vals {
		fmt.Printf("sin %x %.17g\n", math.Float64bits(v), math.Sin(v))
		fmt.Printf("cos %x %.17g\n", math.Float64bits(v), math.Cos(v))
		fmt.Printf("tan %x %.17g\n", math.Float64bits(v), math.Tan(v))
		fmt.Printf("exp %x %.17g\n", math.Float64bits(v), math.Exp(v))
		fmt.Printf("log %x %.17g\n", math.Float64bits(v), math.Log(v))
		fmt.Printf("asin %x %.17g\n", math.Float64bits(v), math.Asin(v))
		fmt.Printf("atan %x %.17g\n", math.Float64bits(v), math.Atan(v))
		fmt.Printf("sinh %x %.17g\n", math.Float64bits(v), math.Sinh(v))
		fmt.Printf("cosh %x %.17g\n", math.Float64bits(v), math.Cosh(v))
		fmt.Printf("tanh %x %.17g\n", math.Float64bits(v), math.Tanh(v))
		fmt.Printf("cbrt %x %.17g\n", math.Float64bits(v), math.Cbrt(v))
		fmt.Printf("expm1 %x %.17g\n", math.Float64bits(v), math.Expm1(v))
		fmt.Printf("log10 %x %.17g\n", math.Float64bits(v), math.Log10(v))
		fmt.Printf("log2 %x %.17g\n", math.Float64bits(v), math.Log2(v))
		fmt.Printf("log1p %x %.17g\n", math.Float64bits(v), math.Log1p(v))
	}
	// Special values: NaN payloads, infinities.
	nanPayloads := []uint64{0x7ff8000000000001, 0x7ff8000000000002, 0xfff8000000000001}
	for _, b := range nanPayloads {
		v := math.Float64frombits(b)
		fmt.Printf("nan sin %x %x\n", b, math.Float64bits(math.Sin(v)))
		fmt.Printf("nan exp %x %x\n", b, math.Float64bits(math.Exp(v)))
		fmt.Printf("nan log %x %x\n", b, math.Float64bits(math.Log(v)))
	}
	for _, v := range []float64{math.Inf(1), math.Inf(-1)} {
		fmt.Printf("inf sin %x %.17g\n", math.Float64bits(v), math.Sin(v))
		fmt.Printf("inf exp %x %.17g\n", math.Float64bits(v), math.Exp(v))
		fmt.Printf("inf log %x %.17g\n", math.Float64bits(v), math.Log(v))
	}
}
