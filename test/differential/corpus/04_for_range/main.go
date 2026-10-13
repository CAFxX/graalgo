package main

import "fmt"

func main() {
	for i := 0; i < 5; i++ {
		fmt.Print(i, " ")
	}
	fmt.Println()
	total := 0
	for _, v := range []int{1, 2, 3} {
		total += v
	}
	fmt.Println(total)
	// range over int (Go 1.22+)
	n := 0
	for i := range 4 {
		n += i
	}
	fmt.Println(n)
}
