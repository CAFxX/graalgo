package main

import (
	"fmt"
	"sort"
)

func main() {
	m := map[string]int{"pear": 3, "apple": 5, "fig": 1, "date": 4}
	// sorted iteration: deterministic output from a map
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println(k, m[k])
	}
	v, ok := m["apple"]
	fmt.Println(v, ok)
	v, ok = m["missing"]
	fmt.Println(v, ok)
	delete(m, "fig")
	fmt.Println(len(m))
	m["grape"] = 9
	fmt.Println(m["grape"])
}
