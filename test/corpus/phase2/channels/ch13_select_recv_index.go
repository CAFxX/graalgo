package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- 42
	buf := [3]int{}
	select {
	case buf[1] = <-ch:
		fmt.Println("index", buf)
	default:
		fmt.Println("default")
	}
}
