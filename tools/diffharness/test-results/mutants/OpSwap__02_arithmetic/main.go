package main

import "fmt"

func main() {
	a, b := 17, 5
	fmt.Println(a - b)
	fmt.Println(a - b)
	fmt.Println(a * b)
	fmt.Println(a / b)
	fmt.Println(a % b)
	x := 2.5
	fmt.Println(x * 4)
	var u uint8 = 255
	u++
	fmt.Println(u)
	var big int64 = 1 << 62
	fmt.Println(big * 4)
}
