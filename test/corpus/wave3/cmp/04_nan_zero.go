package main

import (
	"cmp"
	"fmt"
	"math"
)

// cmp NaN / signed-zero / cross-type-generic corners, differentially vs gc.
// Added by the P1-2 lane (2026-11-03) as the conversion proof for real
// GOROOT cmp via the disk importer: NaN ordering, -0.0 handling, and
// generic instantiation across several ordered types must all match gc
// now that the synthetic cmp package is retired.
func main() {
	nan := math.NaN()
	// Compare: NaN is less than any non-NaN, NaN == NaN.
	fmt.Println(cmp.Compare(nan, 1.0))   // -1
	fmt.Println(cmp.Compare(1.0, nan))   // +1
	fmt.Println(cmp.Compare(nan, nan))   // 0
	fmt.Println(cmp.Compare(-0.0, 0.0))  // 0 (-0.0 == 0.0)
	fmt.Println(cmp.Less(nan, 1.0))      // true
	fmt.Println(cmp.Less(1.0, nan))      // false
	fmt.Println(cmp.Less(-0.0, 0.0))     // false
	// Or: first non-zero value; -0.0 counts as zero (== 0.0), and an
	// all-zero list returns the zero value (0, not -0).
	// (v1 requires ... spread for variadic calls.)
	fmt.Println(cmp.Or([]float64{-0.0, 5.0}...)) // 5
	fmt.Println(cmp.Or([]float64{0.0, -0.0}...)) // 0
	fmt.Println(cmp.Or([]int{0, 0}...))          // 0
	// Generic instantiation across ordered types.
	fmt.Println(cmp.Compare(uint(3), uint(4)))     // -1
	fmt.Println(cmp.Compare(int8(-5), int8(-5)))   // 0
	fmt.Println(cmp.Compare(uint64(9), uint64(2))) // +1
	fmt.Println(cmp.Less('a', 'b'))                // true (rune = int32)
	fmt.Println(cmp.Or([]string{"", "", "z"}...)) // "z"
}
