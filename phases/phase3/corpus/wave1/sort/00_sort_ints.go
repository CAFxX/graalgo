package main

import "sort"

// so00: sort.Sort on IntSlice — shuffled distinct ints, plus Len 0/1.

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
	xs := []int{17, 3, 42, 8, 23, 1, 99, 55, 12, 4, 71, 30}
	sort.Sort(sort.IntSlice(xs))
	printInts(xs)
	println(sort.IsSorted(sort.IntSlice(xs))) // true
	// Empty and single-element are no-ops.
	empty := []int{}
	sort.Sort(sort.IntSlice(empty))
	println(len(empty)) // 0
	one := []int{5}
	sort.Sort(sort.IntSlice(one))
	println(one[0]) // 5
	// Convenience method form.
	ys := sort.IntSlice([]int{9, 2, 7})
	ys.Sort()
	printInts(ys)
}
