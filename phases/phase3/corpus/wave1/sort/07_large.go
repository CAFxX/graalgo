package main

import "sort"

// so07: large deterministic input — 5000 ints from a fixed-seed LCG,
// Sort, print the whole result (byte-compared against gc's pdqsort).

func main() {
	// NB: v1 has no function-local const declarations — var.
	n := 5000
	xs := make([]int, n)
	seed := uint64(0x12345678)
	for i := range xs {
		// LCG, fixed seed: deterministic.
		seed = seed*6364136223846793005 + 1442695040888963407
		xs[i] = int(seed >> 33)
	}
	println(sort.IntsAreSorted(xs)) // false (overwhelmingly likely; fixed seed)
	sort.Sort(sort.IntSlice(xs))
	println(sort.IntsAreSorted(xs)) // true
	// Print first 8, last 8, and a checksum-ish fold.
	for i := 0; i < 8; i++ {
		print(xs[i], " ")
	}
	println()
	for i := n - 8; i < n; i++ {
		print(xs[i], " ")
	}
	println()
	sum := 0
	for _, x := range xs {
		sum += x
	}
	println(sum)
}
