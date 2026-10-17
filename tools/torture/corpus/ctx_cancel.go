package main

import (
	"context"
	"fmt"
)

// ctx_cancel: deterministic context-cancellation pattern.
//
// Eight workers block on ctx.Done(); after cancel() each reports through a
// buffered channel and the main goroutine sums the worker ids. The sum
// (0+1+...+7 = 28) is schedule-independent, so the printed line is
// identical under graalgo and gc. The stress-differential script compares
// stdout byte-for-byte.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 8)
	for w := 0; w < 8; w++ {
		go func(w int) {
			<-ctx.Done()
			done <- w
		}(w)
	}
	cancel()
	sum := 0
	for w := 0; w < 8; w++ {
		sum += <-done
	}
	if ctx.Err() != context.Canceled {
		fmt.Println("wrong-err")
		return
	}
	fmt.Println("canceled", sum)
}
