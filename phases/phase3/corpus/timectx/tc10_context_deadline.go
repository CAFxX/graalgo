package main

import (
	"context"
	"time"
)

// tc10: context WithDeadline — future deadline not expired, past deadline
// expired. (Context.Deadline() is multi-result — not in M5 scope.)
func main() {
	future, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	if future.Err() == nil {
		println("future-not-expired")
	}
	past, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel2()
	<-past.Done()
	if past.Err() == context.DeadlineExceeded {
		println("past-expired")
	}
}
