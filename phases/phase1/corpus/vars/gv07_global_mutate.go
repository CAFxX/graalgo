package main

import "fmt"

var counter int

func bump() {
	counter++
}

func main() {
	bump()
	bump()
	bump()
	fmt.Println(counter)
	counter = 100
	bump()
	fmt.Println(counter)
}
