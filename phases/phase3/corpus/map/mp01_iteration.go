package main

import "fmt"

func main() {
	// Iteration order is unspecified: the corpus asserts the SET of
	// visited keys (order-independent) and looks values up in a
	// canonical order.
	m := map[string]int{}
	keys := []string{"delta", "alpha", "charlie", "bravo"}
	for i, k := range keys {
		m[k] = i
	}
	seen := map[string]bool{}
	count := 0
	for k := range m {
		seen[k] = true
		count++
	}
	fmt.Println(count, len(seen))
	for _, k := range []string{"alpha", "bravo", "charlie", "delta"} {
		fmt.Println(k, m[k], seen[k])
	}
}
