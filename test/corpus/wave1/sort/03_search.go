package main

import "sort"

// so03: binary search — Search, SearchInts, SearchFloat64s,
// SearchStrings, and the (x Search) methods.

func main() {
	xs := []int{1, 3, 5, 7, 9, 11}
	println(sort.SearchInts(xs, 7))  // 3
	println(sort.SearchInts(xs, 1))  // 0
	println(sort.SearchInts(xs, 11)) // 5
	println(sort.SearchInts(xs, 6))  // 3 (insertion point)
	println(sort.SearchInts(xs, 0))  // 0
	println(sort.SearchInts(xs, 12)) // 6 == len
	println(sort.SearchInts([]int{}, 5)) // 0
	// Generic Search with a closure.
	i := sort.Search(len(xs), func(i int) bool { return xs[i] >= 8 })
	println(i) // 4
	// Float64s and Strings.
	fs := []float64{1.5, 2.5, 3.5}
	println(sort.SearchFloat64s(fs, 2.5)) // 1
	println(sort.SearchFloat64s(fs, 2.0)) // 1
	ss := []string{"apple", "fig", "pear"}
	println(sort.SearchStrings(ss, "fig"))    // 1
	println(sort.SearchStrings(ss, "banana")) // 1
	// Method forms.
	println(sort.IntSlice(xs).Search(9))        // 4
	println(sort.Float64Slice(fs).Search(3.5))  // 2
	println(sort.StringSlice(ss).Search("zzz")) // 3
}
