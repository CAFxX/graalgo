package main

import "context"

// tc08: context WithCancel — cancel closes Done, Err is Canceled.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-ctx.Done()
	if ctx.Err() == context.Canceled {
		println("canceled")
	} else {
		println("wrong-err")
	}
}
