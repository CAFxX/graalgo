package main

import "fmt"

func counter() func() int {
	n := 0
	return func() int { n++; return n }
}

func main() {
	c := counter()
	fmt.Println(c(), c(), c())
	add := func(a, b int) int { return a + b }
	fmt.Println(add(3, 4))
	ops := []func(int) int{
		func(x int) int { return x * 2 },
		func(x int) int { return x + 10 },
	}
	fmt.Println(ops[0](5), ops[1](5))
}
