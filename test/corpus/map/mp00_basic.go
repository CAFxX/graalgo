package main

import "fmt"

func main() {
	m := map[string]int{"a": 1, "b": 2}
	fmt.Println(len(m))
	fmt.Println(m["a"], m["missing"])
	v, ok := m["b"]
	fmt.Println(v, ok)
	v, ok = m["missing"]
	fmt.Println(v, ok)
	m["c"] = 3
	delete(m, "a")
	fmt.Println(len(m), m["c"])
	delete(m, "nonexistent")
	fmt.Println(len(m))

	// int keys, overwrite.
	n := make(map[int]string)
	n[1] = "one"
	n[1] = "uno"
	fmt.Println(n[1], len(n))
	n2 := make(map[int]string)
	fmt.Println(len(n2))
}
