package main

import "context"

// tc14: context WithCancelCause and Cause.
func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)
	<-ctx.Done()
	if context.Cause(ctx) == context.Canceled {
		println("cause-ok")
	}
	if ctx.Err() == context.Canceled {
		println("err-is-canceled")
	}
}
