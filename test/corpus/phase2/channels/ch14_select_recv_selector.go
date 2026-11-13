package main

import "fmt"

type S struct{ F int }

func main() {
	ch := make(chan int, 1)
	ch <- 7
	s := &S{}
	select {
	case s.F = <-ch:
		fmt.Println("selector", s.F)
	default:
		fmt.Println("default")
	}
}
