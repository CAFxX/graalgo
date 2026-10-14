package main

import "fmt"

func main() {
	ch1 := make(chan int)
	close(ch1)
	ch2 := make(chan int, 1)
	ch2 <- 99
	select {
	case ch1 <- 1:
		fmt.Println("sent ch1")
	case v := <-ch2:
		fmt.Println("recv ch2", v)
	}
}
