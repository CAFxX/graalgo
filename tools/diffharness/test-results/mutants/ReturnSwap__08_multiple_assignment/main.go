package main

import "fmt"

func divmod(a, b int) (int, int) { return a % b, a / b }

func main() {
	q, r := divmod(17, 5)
	fmt.Println(q, r)
	x, y := 1, 2
	x, y = y, x
	fmt.Println(x, y)
}
