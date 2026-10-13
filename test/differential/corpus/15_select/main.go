package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan string, 1)
	ch <- "ready"
	select {
	case msg := <-ch:
		fmt.Println("got", msg)
	case <-time.After(time.Second):
		fmt.Println("timeout")
	}
	// select with default: non-blocking
	select {
	case v := <-ch:
		fmt.Println("unexpected", v)
	default:
		fmt.Println("empty")
	}
	// timeout branch fires deterministically
	select {
	case <-ch:
		fmt.Println("unexpected")
	case <-time.After(10 * time.Millisecond):
		fmt.Println("timed out")
	}
}
