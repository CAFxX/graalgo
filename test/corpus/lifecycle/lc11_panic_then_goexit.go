package main

import (
	"fmt"
	"runtime"
)

func main() {
	go func() {
		defer fmt.Println("worker deferred")
		defer func() { runtime.Goexit() }()
		panic("cancelled by goexit")
	}()
	c := make(chan int)
	<-c
}
