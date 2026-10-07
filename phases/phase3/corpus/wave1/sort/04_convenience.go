package main

import "sort"

// so04: convenience wrappers — Ints, Float64s, Strings and the
// AreSorted variants.

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
	xs := []int{4, 2, 9, 1, 7}
	println(sort.IntsAreSorted(xs)) // false
	sort.Ints(xs)
	printInts(xs)                   // 1 2 4 7 9
	println(sort.IntsAreSorted(xs)) // true

	fs := []float64{3.25, 1.5, 2.75}
	sort.Float64s(fs)
	println(fs[0], fs[1], fs[2]) // 1.5 2.75 3.25
	println(sort.Float64sAreSorted(fs)) // true
	println(sort.Float64sAreSorted([]float64{2, 1})) // false

	ss := []string{"delta", "alpha", "charlie", "bravo"}
	println(sort.StringsAreSorted(ss)) // false
	sort.Strings(ss)
	for i, s := range ss {
		if i > 0 {
			print(" ")
		}
		print(s)
	}
	println() // alpha bravo charlie delta
	println(sort.StringsAreSorted(ss)) // true
}
