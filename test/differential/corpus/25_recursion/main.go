package main

import "fmt"

var memo = map[int]int{0: 0, 1: 1}

func fib(n int) int {
	if v, ok := memo[n]; ok {
		return v
	}
	memo[n] = fib(n-1) + fib(n-2)
	return memo[n]
}

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

func main() {
	fmt.Println(fib(20))
	fmt.Println(fact(10))
	fmt.Println(len(memo))
}
