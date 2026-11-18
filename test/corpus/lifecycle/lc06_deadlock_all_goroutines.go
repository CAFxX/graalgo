package main

import "fmt"

func worker(c chan int) {
	c <- 1
}

func main() {
	c := make(chan int)
	d := make(chan int)
	go worker(c)
	go worker(c)
	fmt.Println(<-c)
	<-d
}
