package main

import "fmt"

func main() {
	var m map[string]int
	// Reads and deletes on a nil map are fine.
	fmt.Println(m["x"], len(m))
	_, ok := m["x"]
	fmt.Println(ok)
	delete(m, "x")
	fmt.Println("read-delete ok")
	// Write panics.
	m["x"] = 1
}
