package main

import "context"

// tc11: context WithValue — key lookup and missing-key nil.
func main() {
	ctx := context.WithValue(context.Background(), "key", "val")
	if ctx.Value("key") == "val" {
		println("value-ok")
	}
	if ctx.Value("missing") == nil {
		println("missing-nil")
	}
	// Shadowing: child overrides parent key.
	child := context.WithValue(ctx, "key", "child-val")
	if child.Value("key") == "child-val" {
		println("shadow-ok")
	}
}
