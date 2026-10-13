package main

import "sort"

// so02: sort.Reverse — descending sort via the reversed Interface.

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
	xs := []int{5, 1, 8, 3, 9, 2, 7, 4, 6}
	sort.Sort(sort.Reverse(sort.IntSlice(xs)))
	printInts(xs) // 9 8 7 6 5 4 3 2 1
	println(sort.IsSorted(sort.Reverse(sort.IntSlice(xs)))) // false
	// Reverse of reverse is the original order.
	ys := []int{3, 1, 2}
	sort.Sort(sort.IntSlice(ys))
	sort.Sort(sort.Reverse(sort.Reverse(sort.IntSlice(ys))))
	printInts(ys) // 1 2 3
	// Strings descending.
	ss := []string{"pear", "apple", "fig", "banana"}
	sort.Sort(sort.Reverse(sort.StringSlice(ss)))
	for i, s := range ss {
		if i > 0 {
			print(" ")
		}
		print(s)
	}
	println() // pear fig banana apple
}
