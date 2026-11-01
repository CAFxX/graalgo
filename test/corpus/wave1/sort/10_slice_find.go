package main

import "sort"

// so10: sort.Slice/SliceStable/SliceIsSorted/Find — the former M7
// fail-fast members, now real implementations (the Slice family on the
// reflectlite Swapper path, Find via the generic MR pair transform).
// Differential vs stock gc 1.27.1.

// NOTE (backend boundary, not a frontend gap): Slice/SliceStable run
// here only over integer/string/byte element kinds. Struct, array,
// pointer, float64, and bool element kinds fail at RUNTIME in the
// backend reflect Swapper (three distinct backend errors, all
// reproduced in isolation 2026-11-01): struct elements die in
// reflect.typedmemmove ("aggregate store through a byte-offset view")
// via sort.Slice, and in a direct reflect.Swapper call with
// "ArrayRef cannot be cast to GoStruct"; pointer elements die with
// "GoPointer cannot be cast to Long"; float64 with "[D cannot be
// cast to [J"; bool with "[Z cannot be cast to [B". The frontend
// accepts all of these (see TestSyntheticSortSliceSucceeds, which
// covers a struct-slice call at export level); the backend Swapper
// needs a lane of its own. Stability observability needs
// payload-carrying elements (structs/pointers), so SliceStable is
// covered here for sort correctness on scalars only — its stability
// property on struct payloads is exactly the blocked sub-case.

func printInts(xs []int) {
	for i, x := range xs {
		if i > 0 {
			print(" ")
		}
		print(x)
	}
	println()
}

func printStrings(ss []string) {
	for i, s := range ss {
		if i > 0 {
			print(" ")
		}
		print(s)
	}
	println()
}

func main() {
	// Slice on ints: duplicates, already sorted, reverse sorted.
	xs := []int{5, 2, 8, 2, 9, 1, 5}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	printInts(xs) // 1 2 2 5 5 8 9
	// SliceIsSorted: true, false, empty, single.
	println(sort.SliceIsSorted(xs, func(i, j int) bool { return xs[i] < xs[j] })) // true
	unsorted := []int{2, 1}
	println(sort.SliceIsSorted(unsorted, func(i, j int) bool { return unsorted[i] < unsorted[j] })) // false
	println(sort.SliceIsSorted([]int{}, func(i, j int) bool { return false }))                      // true
	one := []int{7}
	println(sort.SliceIsSorted(one, func(i, j int) bool { return false })) // true
	// Slice on strings.
	ss := []string{"pear", "apple", "fig", "apple"}
	sort.Slice(ss, func(i, j int) bool { return ss[i] < ss[j] })
	printStrings(ss) // apple apple fig pear
	// SliceStable on scalars (sort correctness; stability itself is
	// unobservable without payload elements — see the note above).
	dups := []int{3, 1, 2, 1, 3, 2}
	sort.SliceStable(dups, func(i, j int) bool { return dups[i] < dups[j] })
	printInts(dups) // 1 1 2 2 3 3
	// SliceStable on strings with duplicates.
	sdups := []string{"b", "a", "b", "a"}
	sort.SliceStable(sdups, func(i, j int) bool { return sdups[i] < sdups[j] })
	printStrings(sdups) // a a b b
	// Slice on int32 (another working integer kind).
	xs32 := []int32{5, 2, 8}
	sort.Slice(xs32, func(i, j int) bool { return xs32[i] < xs32[j] })
	println(xs32[0], xs32[1], xs32[2]) // 2 5 8
	// Slice on an empty and a single-element slice are no-ops.
	empty := []int{}
	sort.Slice(empty, func(i, j int) bool { return false })
	println(len(empty)) // 0
	single := []int{42}
	sort.Slice(single, func(i, j int) bool { return false })
	println(single[0]) // 42
	// Find: cmp follows gc's convention — > 0 on a leading prefix,
	// == 0 in the middle, < 0 on the suffix (target minus entry).
	// Found in the middle, at 0, at n-1; not found (all positive,
	// all negative); n = 0 (cmp never called).
	i, found := sort.Find(10, func(i int) int { return 5 - i })
	println(i, found) // 5 true
	i, found = sort.Find(10, func(i int) int { return -i })
	println(i, found) // 0 true
	i, found = sort.Find(10, func(i int) int { return 9 - i })
	println(i, found) // 9 true
	i, found = sort.Find(10, func(i int) int { return 1 })
	println(i, found) // 10 false
	i, found = sort.Find(10, func(i int) int { return -1 })
	println(i, found) // 0 false
	i, found = sort.Find(0, func(i int) int { return 0 })
	println(i, found) // 0 false
	// Find over a sorted slice through an indexing cmp (target - entry).
	sorted := []int{1, 3, 5, 7, 9}
	i, found = sort.Find(len(sorted), func(i int) int { return 7 - sorted[i] })
	println(i, found) // 3 true
	i, found = sort.Find(len(sorted), func(i int) int { return 6 - sorted[i] })
	println(i, found) // 3 false
	i, found = sort.Find(len(sorted), func(i int) int { return 0 - sorted[i] })
	println(i, found) // 0 false
	i, found = sort.Find(len(sorted), func(i int) int { return 10 - sorted[i] })
	println(i, found) // 5 false
	// Large Slice: 200-element fixed-seed LCG shuffle (exercises the
	// pdqsort ninther path, shortestNinther = 50), then SliceIsSorted.
	n := 200
	big := make([]int, n)
	seed := uint64(0x12345678)
	for k := range big {
		seed = seed*6364136223846793005 + 1442695040888963407
		big[k] = int(seed >> 33)
	}
	println(sort.SliceIsSorted(big, func(i, j int) bool { return big[i] < big[j] })) // false
	sort.Slice(big, func(i, j int) bool { return big[i] < big[j] })
	println(sort.SliceIsSorted(big, func(i, j int) bool { return big[i] < big[j] })) // true
	for k := 0; k < 5; k++ {
		print(big[k], " ")
	}
	println()
	for k := n - 5; k < n; k++ {
		print(big[k], " ")
	}
	println()
}
