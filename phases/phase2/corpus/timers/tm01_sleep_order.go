package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan string, 3)
	go func() { time.Sleep(150 * time.Millisecond); done <- "slow" }()
	go func() { time.Sleep(50 * time.Millisecond); done <- "fast" }()
	go func() { time.Sleep(100 * time.Millisecond); done <- "mid" }()
	for i := 0; i < 3; i++ {
		fmt.Println(<-done)
	}
}
