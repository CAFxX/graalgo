package main

import (
	"fmt"
	"slices"
)

func main() {
	// slices.Contains: reports whether v is present in s
	fmt.Println(slices.Contains([]int{1, 2, 3}, 2))    // true
	fmt.Println(slices.Contains([]int{1, 2, 3}, 5))    // false
	fmt.Println(slices.Contains([]string{"a"}, "a"))   // true
	fmt.Println(slices.Contains([]string{"a"}, "b"))   // false
	fmt.Println(slices.Contains([]int{}, 1))           // false

	// slices.Index: returns index of v, or -1
	fmt.Println(slices.Index([]int{1, 2, 3}, 2))  // 1
	fmt.Println(slices.Index([]int{1, 2, 3}, 5))  // -1
}
