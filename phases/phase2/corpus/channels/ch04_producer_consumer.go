package main

import "fmt"

func producer(ch chan int) {
	for i := 1; i <= 5; i++ {
		ch <- i * i
	}
	close(ch)
}

func main() {
	ch := make(chan int, 5)
	go producer(ch)
	sum := 0
	for v := range ch {
		sum += v
	}
	fmt.Println(sum)
}
