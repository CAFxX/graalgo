// 58_float_minmax_signed_zero: the min/max builtins on signed zeros and
// NaN. Go spec: max(-0.0, 0.0) is 0.0 and min(-0.0, 0.0) is -0.0 ("negative
// zero is less than positive zero" for min/max), any NaN operand yields NaN.
// Split out of 52_float_edges: graalgo returns -0 for max(-0, 0).
package main

import (
	"fmt"
	"math"
)

func main() {
	zero := 0.0
	negz := math.Copysign(0, -1)
	nan := math.NaN()
	fmt.Println(max(negz, zero), max(zero, negz), min(negz, zero), min(zero, negz))
	fmt.Println(math.Signbit(max(negz, zero)), math.Signbit(min(zero, negz)))
	fmt.Println(max(nan, 1), min(1, nan), max(negz, negz), min(zero, zero))
	f, g := float32(negz), float32(zero)
	fmt.Println(max(f, g), min(f, g), math.Signbit(float64(max(f, g))))
}
