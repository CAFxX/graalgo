package main

import "fmt"

// Build n elements one append at a time. The slice escapes via the
// return, so gc's stack-buffer append optimization does not apply and
// caps follow the runtime growslice formula deterministically.
func build(n int) []int {
	var s []int
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	for _, n := range []int{1, 2, 3, 4, 5, 6, 8, 9, 16, 17, 100, 300} {
		s := build(n)
		fmt.Println(n, len(s), cap(s))
	}
}
