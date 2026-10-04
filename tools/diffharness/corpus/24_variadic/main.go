package main

import "fmt"

func sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func main() {
	fmt.Println(sum())
	fmt.Println(sum(1, 2, 3))
	s := []int{4, 5, 6}
	fmt.Println(sum(s...))
	fmt.Println(sum(append(s, 7)...))
}
