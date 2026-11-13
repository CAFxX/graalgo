package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- 9
	x := 0
	p := &x
	select {
	case *p = <-ch:
		fmt.Println("star", x)
	default:
		fmt.Println("default")
	}
}
