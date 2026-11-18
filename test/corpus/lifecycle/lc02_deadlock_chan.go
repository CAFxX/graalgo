package main

import "fmt"

func main() {
	fmt.Println("start")
	c := make(chan int)
	<-c
}
