package main

import (
	"cmp"
	"fmt"
)

func main() {
	// cmp.Compare: ordered comparison returning -1, 0, +1
	fmt.Println(cmp.Compare(1, 2))     // -1
	fmt.Println(cmp.Compare(2, 1))     // +1
	fmt.Println(cmp.Compare(5, 5))     // 0
	fmt.Println(cmp.Compare("a", "b")) // -1
	fmt.Println(cmp.Compare("b", "a")) // +1
	fmt.Println(cmp.Compare("", ""))   // 0
	fmt.Println(cmp.Compare(1.5, 2.5)) // -1
}
