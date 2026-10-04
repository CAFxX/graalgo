package main

import "fmt"

// Defer arguments are evaluated immediately, at defer time.
func main() {
	x := 1
	defer fmt.Println("deferred x =", x)
	x = 2
	defer func() { fmt.Println("closure sees x =", x) }()
	fmt.Println("body x =", x)
}
