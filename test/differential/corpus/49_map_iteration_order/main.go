package main

import "fmt"

// Map iteration order is unspecified (Go spec: "The iteration order over
// maps is not specified"); gc randomizes it per iteration. This case pins
// the randomization: over many iterations of the same map, at least two
// distinct orders must appear. The printed verdict is deterministic, so the
// differential harness's exact-stdout comparison stays valid; a backend
// with fixed (table) iteration order fails here with a panic instead.
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8}
	seen := make(map[string]bool)
	for iter := 0; iter < 50; iter++ {
		s := ""
		for k := range m {
			s += k
		}
		seen[s] = true
	}
	if len(seen) < 2 {
		panic("map iteration order did not vary across 50 iterations")
	}
	fmt.Println("map iteration order varied")
}
