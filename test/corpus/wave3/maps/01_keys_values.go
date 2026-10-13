package main

import (
	"fmt"
	"maps"
	"slices"
)

func main() {
	m := map[string]int{"b": 2, "a": 1, "c": 3}
	// maps.Keys: iterator over map keys (sorted for determinism)
	fmt.Println(slices.Sorted(maps.Keys(m))) // [a b c]
	// maps.Values: iterator over map values (sorted for determinism)
	fmt.Println(slices.Sorted(maps.Values(m))) // [1 2 3]
}
