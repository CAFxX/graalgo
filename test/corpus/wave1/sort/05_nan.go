package main

import "sort"

// so05: adversarial float64 — NaN sorts before every other value
// (Float64Slice.Less), and Search on NaN-containing slices.
// (math is not a synthetic package: NaN/Inf come from IEEE division.)

func main() {
	zero := 0.0
	nan := zero / zero
	posInf := 1.0 / zero
	negInf := -1.0 / zero
	xs := []float64{3.0, nan, 1.0, nan, 2.0, negInf, posInf, 0.0}
	sort.Sort(sort.Float64Slice(xs))
	// NaNs first (both of them), then -Inf, 0, 1, 2, 3, +Inf.
	for i, x := range xs {
		if i > 0 {
			print(" ")
		}
		if x != x {
			print("NaN")
		} else {
			print(x)
		}
	}
	println()
	println(sort.Float64sAreSorted(xs)) // true
	// Float64s convenience: same NaN-first rule.
	ys := []float64{1.0, nan, 0.5}
	sort.Float64s(ys)
	println(ys[0] != ys[0]) // true: NaN first
	println(ys[1] == 0.5 && ys[2] == 1.0)
	// Search for 1.0 in a NaN-prefixed sorted slice.
	zs := []float64{nan, 0.5, 1.0, 2.0}
	println(sort.SearchFloat64s(zs, 1.0)) // 2
}
