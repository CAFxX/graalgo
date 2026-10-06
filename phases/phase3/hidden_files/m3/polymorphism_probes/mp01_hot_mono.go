package main

import "fmt"

type Adder interface{ Add(x int) int }

type C struct{ base int }

func (c C) Add(x int) int { return c.base + x }

func main() {
	var a Adder = C{base: 100}
	sum := 0
	for i := 0; i < 1000; i++ {
		sum += a.Add(i)
	}
	fmt.Println(sum)
}
