package main

import "fmt"

func main() {
	defer fmt.Println("first")
	defer fmt.Println("second")
	fmt.Println("body")
	for i := 0; i < 3; i++ {
		defer fmt.Println("loop", i)
	}
}
