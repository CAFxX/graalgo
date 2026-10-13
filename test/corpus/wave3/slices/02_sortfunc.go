package main

import (
	"cmp"
	"fmt"
	"slices"
)

func main() {
	// slices.SortFunc: sorts using a comparison function
	s1 := []int{3, 1, 2}
	slices.SortFunc(s1, func(a, b int) int {
		return cmp.Compare(a, b)
	})
	fmt.Println(s1) // [1 2 3]

	// Reverse sort
	s2 := []int{1, 2, 3}
	slices.SortFunc(s2, func(a, b int) int {
		return cmp.Compare(b, a)
	})
	fmt.Println(s2) // [3 2 1]
}
