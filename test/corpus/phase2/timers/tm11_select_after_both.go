package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan string, 1)
	ch <- "fast"
	select {
	case v := <-ch:
		fmt.Println("value", v)
	case <-time.After(100 * time.Millisecond):
		fmt.Println("timeout (bug)")
	}
	select {
	case v := <-ch:
		fmt.Println("value (bug)", v)
	case <-time.After(100 * time.Millisecond):
		fmt.Println("timeout")
	}
}
