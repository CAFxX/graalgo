package main

import "fmt"

var order []string

func note(s string) int {
	order = append(order, s)
	return len(order)
}

var _ = note("blank-init")
var x = note("x-init")

func main() {
	fmt.Println(order)
	fmt.Println(x)
}
