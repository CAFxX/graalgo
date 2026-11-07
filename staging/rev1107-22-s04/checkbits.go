package main

import (
	"fmt"
	"math"
)

func main() {
	vals := []float64{1e21, 1e20, 123456, 1234567, 1e-4, 1e-5, 123456789.123456789, 1.7976931348623157e308, 2.2250738585072014e-308, 1e-323, 0.15, 0.3, 1.0 / 3, 2.0 / 3, math.Pi, 1e15 + 0.5, 999999.5, 100000, 1000000, 0.45, 0.35, 0.4, 0.7, 1.1, 1.2, 1.6, 1e-300}
	for _, v := range vals {
		fmt.Printf("%v -> 0x%016X\n", v, math.Float64bits(v))
	}
	f32s := []float32{0.1, 1e-7, 1e-8}
	for _, v := range f32s {
		fmt.Printf("%v (f32) -> 0x%08X\n", v, math.Float32bits(v))
	}
}
