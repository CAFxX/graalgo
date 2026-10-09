package main

import (
	"fmt"
	"maps"
)

func main() {
	m1 := map[string]int{"a": 1, "b": 2}
	// maps.Clone: returns a copy
	m2 := maps.Clone(m1)
	fmt.Println(maps.Equal(m1, m2)) // true
	m2["c"] = 3
	fmt.Println(maps.Equal(m1, m2)) // false
	fmt.Println(len(m1), len(m2))   // 2 3

	// maps.Equal: nil handling
	var nilMap map[string]int
	fmt.Println(maps.Equal(nilMap, nilMap)) // true
	fmt.Println(maps.Equal(m1, nilMap))     // false
}
