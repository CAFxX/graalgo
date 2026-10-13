package main

import (
	"fmt"
	"slices"
)

func main() {
	// slices.Values: iterator over slice values
	s := []int{10, 20, 30}
	for v := range slices.Values(s) {
		fmt.Println(v)
	}
	// slices.All: iterator over index/value pairs
	for i, v := range slices.All(s) {
		fmt.Println(i, v)
	}
	// slices.Sorted: collect iterator values into sorted slice
	fmt.Println(slices.Sorted(slices.Values([]int{3, 1, 2})))
}
