package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- 5
	m := map[string]int{}
	select {
	case m["k"] = <-ch:
		fmt.Println("map", m)
	default:
		fmt.Println("default")
	}
}
