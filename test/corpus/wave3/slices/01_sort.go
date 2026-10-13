package main

import (
	"fmt"
	"slices"
)

func main() {
	// slices.Sort: sorts a slice of ordered elements
	s1 := []int{3, 1, 2}
	slices.Sort(s1)
	fmt.Println(s1) // [1 2 3]

	s2 := []string{"banana", "apple", "cherry"}
	slices.Sort(s2)
	fmt.Println(s2) // [apple banana cherry]

	s3 := []int{5, 5, 5}
	slices.Sort(s3)
	fmt.Println(s3) // [5 5 5]

	s4 := []int{}
	slices.Sort(s4)
	fmt.Println(s4) // []
}
