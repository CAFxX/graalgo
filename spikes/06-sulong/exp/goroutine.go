package main

import "fmt"

func worker(ch chan int) {
	sum := 0
	for i := 0; i < 100; i++ {
		sum += i
	}
	ch <- sum
}

func main() {
	ch := make(chan int)
	go worker(ch)
	fmt.Println("result:", <-ch)
}
