package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	select {
	case ch <- 99:
		fmt.Println("sent")
	default:
		fmt.Println("default")
	}
	fmt.Println(<-ch)
}
