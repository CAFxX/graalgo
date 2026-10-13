package main

import (
	"cmp"
	"fmt"
)

func main() {
	// cmp.Less: strict less-than predicate
	fmt.Println(cmp.Less(1, 2))     // true
	fmt.Println(cmp.Less(2, 1))     // false
	fmt.Println(cmp.Less(5, 5))     // false
	fmt.Println(cmp.Less("a", "b")) // true
	fmt.Println(cmp.Less("b", "b")) // false
}
