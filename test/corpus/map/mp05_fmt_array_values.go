package main

import "fmt"

// bug-041: fmt %v of map with array-typed values crashed with
// ClassCastException ([J cannot be cast to [B) because walkMapChain
// returned an empty element-kind chain for non-nested-map values,
// causing printSlice to default the element kind to UINT8.
func main() {
	m := map[int][2]int{2: {2, 2}, 1: {1, 1}}
	fmt.Printf("%v\n", m)

	// Slice values also exercise the fixed chain path.
	s := map[int][]int{2: {2, 2}, 1: {1}}
	fmt.Printf("%v\n", s)

	// Nested maps still work (regression guard for the chain walk).
	n := map[string]map[string]int{"x": {"b": 2, "a": 1}}
	fmt.Printf("%v\n", n)
}
