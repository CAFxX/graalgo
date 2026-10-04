package main

import "fmt"

// Declared before b, but depends on it: initialization must follow
// dependency order, not source order (Go spec, Package initialization).
var a = b + 1
var b = 2
var c = a * b

func main() {
	fmt.Println(a, b, c)
}
