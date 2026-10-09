package main

import (
	"fmt"
	"slices"
)

func main() {
	// slices.Insert: inserts values at index i (must use ... spread in v1)
	s1 := []int{1, 2, 3}
	s1 = slices.Insert(s1, 1, []int{9}...)
	fmt.Println(s1) // [1 9 2 3]

	// slices.Delete: removes elements s[i:j]
	s2 := []int{1, 2, 3, 4}
	s2 = slices.Delete(s2, 1, 3)
	fmt.Println(s2) // [1 4]

	// slices.Replace: replaces s[i:j] with v (must use ... spread)
	s3 := []int{1, 2, 3}
	s3 = slices.Replace(s3, 1, 2, []int{7, 8}...)
	fmt.Println(s3) // [1 7 8 3]
}
