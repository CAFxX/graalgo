package main

import "fmt"

func main() {
	m := map[string]int{}
	fmt.Println(m["absent"]) // zero value
	m["a"] = 0
	v, ok := m["a"]
	fmt.Println(v, ok) // present-but-zero vs absent
	_, ok = m["absent"]
	fmt.Println(ok)
	var s string
	var b bool
	var sl []int
	fmt.Printf("%q %v %v\n", s, b, sl == nil)
}
