package main

import "context"

// tc13: context.AfterFunc — runs on cancel.
func main() {
	fired := make(chan bool, 1)
	ctx, cancel := context.WithCancel(context.Background())
	context.AfterFunc(ctx, func() { fired <- true })
	cancel()
	<-fired
	println("ctx-afterfunc-fired")
}
