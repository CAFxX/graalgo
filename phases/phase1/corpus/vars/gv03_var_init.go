package main

import "fmt"

var memo = map[int]int{0: 0, 1: 1}

func fib(n int) int {
	if v, ok := memo[n]; ok {
		return v
	}
	v := fib(n-1) + fib(n-2)
	memo[n] = v
	return v
}

func main() {
	fmt.Println(fib(10))
	fmt.Println(len(memo))
}
