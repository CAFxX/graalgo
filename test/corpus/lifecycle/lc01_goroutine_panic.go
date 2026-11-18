package main

import (
	"fmt"
	"time"
)

func main() {
	go func() {
		panic("boom in goroutine")
	}()
	time.Sleep(5 * time.Second)
	fmt.Println("unreachable")
}
