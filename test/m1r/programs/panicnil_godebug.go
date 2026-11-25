//go:debug panicnil=1

package main

// BUG-0046: a //go:debug directive (cmd/go: -X runtime.godebugDefault)
// makes panic(nil) panic with nil: recover returns nil, the GODEBUG
// environment variable overrides the default and dropping it restores the
// default, and the crash report prints "panic: nil".

import (
	"fmt"
	"os"
)

func try(name string, f func()) {
	defer func() {
		r := recover()
		fmt.Printf("%s: %T %v\n", name, r, r)
	}()
	f()
}

func main() {
	try("default", func() { panic(nil) })
	os.Setenv("GODEBUG", "panicnil=0")
	try("env", func() { panic(nil) })
	os.Setenv("GODEBUG", "")
	try("env-cleared", func() { panic(nil) })
	panic(nil)
}
