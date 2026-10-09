package main

import (
	"cmp"
	"fmt"
)

func main() {
	// cmp.Or: returns the first non-zero value (must use ... spread in v1)
	fmt.Println(cmp.Or([]int{0, 5}...))          // 5
	fmt.Println(cmp.Or([]int{0, 0, 7}...))       // 7
	fmt.Println(cmp.Or([]int{3, 0, 7}...))       // 3
	fmt.Println(cmp.Or([]string{"", "a"}...))    // "a"
	fmt.Println(cmp.Or([]string{"x", "a"}...))   // "x"
}
