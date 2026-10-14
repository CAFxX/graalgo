package main

import "fmt"

func spin() {
	x := 0
	for i := 0; i < 50000000; i++ {
		x += i
	}
	_ = x
}

func main() {
	go spin()
	go spin()
	spin()
	fmt.Println("spun")
}
