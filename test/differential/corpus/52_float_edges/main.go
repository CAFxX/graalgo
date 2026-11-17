// 52_float_edges: IEEE 754 semantics as Go defines them: NaN comparisons,
// signed zero, infinities, float32 rounding of every operation, division
// by zero (no panic for floats), formatting of special values.
package main

import (
	"fmt"
	"math"
)

func main() {
	zero := 0.0
	nan := zero / zero
	inf := 1 / zero
	ninf := -1 / zero
	negz := -zero
	fmt.Println(nan, inf, ninf, negz, 1/negz)
	fmt.Println("nan cmp", nan == nan, nan != nan, nan < 1, nan > 1, nan <= nan, nan >= nan)
	fmt.Println("zero cmp", negz == zero, negz < zero, 1/negz < 0, math.Signbit(negz))
	fmt.Println("inf", inf+1, inf-inf, inf*0, inf > math.MaxFloat64, ninf < -math.MaxFloat64)
	big, tiny := math.MaxFloat64, math.SmallestNonzeroFloat64
	fmt.Println("overflow", big*2, -big*2, tiny/2)
	var f32big float32 = 1e38
	var f32 float32 = 16777216
	f32 += 1
	fmt.Println("f32", f32, float32(0.1)+float32(0.2), float32(1)/float32(3), f32big*10)
	a, b := float32(0.1), float32(0.7)
	fmt.Println("f32ops", a*b, a-b, b/a, a+b == float32(0.8))
	x, y := 0.1, 0.2
	fmt.Println("f64ops", x+y, x+y == 0.3, x*3, 1-0.9, 7.0/3.0)
	vals := []float64{1.5, -2.25, 1e21, 1e-7, 123456789.125, nan, inf, negz}
	for _, v := range vals {
		fmt.Printf("%v %g %e %.3f %8.2f %G\n", v, v, v, v, v, v)
	}
	fmt.Println("minmax", min(1.0, nan), max(negz, zero), min(negz, zero), max(1, 2.5))
	fmt.Println("floor", math.Floor(-1.5), math.Ceil(-1.5), math.Trunc(-1.5), math.Round(-2.5), math.Sqrt(2))
	fmt.Println("bits", math.Float64bits(negz), math.Float32bits(float32(nan)) != 0, math.Float64frombits(0x3FF0000000000000))
	h := 0.0
	for i := 1; i <= 20; i++ {
		h += 1.0 / float64(i)
	}
	fmt.Println("harmonic", h)
}
