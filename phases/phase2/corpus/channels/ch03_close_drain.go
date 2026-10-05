package main

import "fmt"

func main() {
	ch := make(chan int, 2)
	ch <- 10
	ch <- 20
	close(ch)
	for i := 0; i < 4; i++ {
		v, ok := <-ch
		fmt.Println(v, ok)
	}
}
