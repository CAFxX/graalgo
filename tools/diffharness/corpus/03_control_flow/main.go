package main

import "fmt"

func classify(n int) string {
	if n < 10 {
		return "small"
	}
	if n < 100 {
		return "medium"
	}
	return "large"
}

func main() {
	for _, n := range []int{5, 10, 99, 100, 101} {
		fmt.Println(n, classify(n))
	}
	switch n := 7; n % 2 {
	case 0:
		fmt.Println("even")
	default:
		fmt.Println("odd")
	}
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 1 {
				continue outer
			}
			fmt.Print(i, j, " ")
		}
	}
	fmt.Println()
}
