package main

import (
	"fmt"
	"time"
)

func main() {
	c := make(chan int)
	time.AfterFunc(50*time.Millisecond, func() { c <- 7 })
	fmt.Println(<-c)
	t := time.NewTicker(10 * time.Millisecond)
	<-t.C
	<-t.C
	t.Stop()
	time.Sleep(20 * time.Millisecond)
	d := make(chan int)
	go func() { time.Sleep(30 * time.Millisecond); d <- 1 }()
	fmt.Println(<-d)
	fmt.Println("ok")
}
