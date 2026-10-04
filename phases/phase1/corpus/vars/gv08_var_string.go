package main

import "fmt"

var greeting = "hello"
var target = "world"

func main() {
	fmt.Println(greeting + ", " + target)
	greeting = "goodbye"
	fmt.Println(greeting + ", " + target)
}
