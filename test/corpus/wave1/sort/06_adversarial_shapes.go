package main

import "sort"

// so06: adversarial input shapes — already sorted (partialInsertionSort
// path), reverse sorted (decreasingHint path), all equal (partitionEqual
// path), duplicates-heavy, single, empty. Duplicate-heavy Sort output is
// compared byte-for-byte against gc's pdqsort: a faithful-port check.

func printInts(xs []int) {
	for i, x := range xs {
		if i > 0 {
			print(" ")
		}
		print(x)
	}
	println()
}

func main() {
	// Already sorted: 100 ascending.
	sorted := make([]int, 100)
	for i := range sorted {
		sorted[i] = i
	}
	sort.Sort(sort.IntSlice(sorted))
	println(sorted[0], sorted[99], sort.IntsAreSorted(sorted)) // 0 99 true

	// Reverse sorted: 100 descending.
	rev := make([]int, 100)
	for i := range rev {
		rev[i] = 100 - i
	}
	sort.Sort(sort.IntSlice(rev))
	println(rev[0], rev[99], sort.IntsAreSorted(rev)) // 1 100 true

	// All equal: 100 copies.
	eq := make([]int, 100)
	for i := range eq {
		eq[i] = 7
	}
	sort.Sort(sort.IntSlice(eq))
	println(eq[0], eq[99], sort.IntsAreSorted(eq)) // 7 7 true

	// Duplicates-heavy: values in {0,1,2}, deterministic pattern.
	dup := make([]int, 200)
	for i := range dup {
		dup[i] = (i*7 + 3) % 3
	}
	sort.Sort(sort.IntSlice(dup))
	printInts(dup[:12]) // 0 0 0 0 0 0 0 0 0 0 0 0
	println(dup[199], sort.IntsAreSorted(dup)) // 2 true

	// Two-element and single-element edge cases.
	two := []int{2, 1}
	sort.Sort(sort.IntSlice(two))
	printInts(two) // 1 2
	one := []int{42}
	sort.Sort(sort.IntSlice(one))
	println(one[0]) // 42
	none := []int{}
	sort.Sort(sort.IntSlice(none))
	println(len(none)) // 0

	// Sawtooth: alternating high/low, 120 elements.
	saw := make([]int, 120)
	for i := range saw {
		if i%2 == 0 {
			saw[i] = i
		} else {
			saw[i] = 120 - i
		}
	}
	sort.Sort(sort.IntSlice(saw))
	println(saw[0], saw[119], sort.IntsAreSorted(saw))
}
