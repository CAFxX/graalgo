package main

import (
	"fmt"
	"time"
)

func main() {
	time.AfterFunc(100*time.Millisecond, func() { fmt.Println("timer fired") })
	t := time.NewTimer(200 * time.Millisecond)
	<-t.C
	fmt.Println("timer received")
	c := make(chan int)
	go func() {
		time.Sleep(50 * time.Millisecond)
		fmt.Println("sleeper woke")
	}()
	<-c
}
