package main

import "fmt"

// User-defined variadic functions (R1 P1-1, R2 P1-2): the spread form
// f(s...) passes the slice directly and is implemented. Non-spread calls
// (argument packing) are a clean WS D scope error at export time, covered
// by MainTest.testCheckRejectsVariadicPacking (they must not be in this
// corpus: the exporter rejects them).
func sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func main() {
	s := []int{1, 2, 3, 4, 5}
	fmt.Println(sum(s...))
	fmt.Println(sum(append(s, 7)...))
	var z []int
	fmt.Println(sum(z...))
}
