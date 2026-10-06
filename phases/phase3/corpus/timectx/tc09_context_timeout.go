package main

import (
	"context"
	"time"
)

// tc09: context WithTimeout — deadline exceeded.
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	<-ctx.Done()
	if ctx.Err() == context.DeadlineExceeded {
		println("deadline-exceeded")
	} else {
		println("wrong-err")
	}
}
