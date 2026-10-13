package main

import "fmt"

func split(sum int) (x, y int) {
	x = sum * 4 / 9
	y = sum - x
	return // naked return of named values
}

func main() {
	a, b := split(17)
	fmt.Println(a, b)
}
