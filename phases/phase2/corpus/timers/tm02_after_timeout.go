package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan int)
	select {
	case v := <-ch:
		fmt.Println("value", v)
	case <-time.After(100 * time.Millisecond):
		fmt.Println("timeout")
	}
}
