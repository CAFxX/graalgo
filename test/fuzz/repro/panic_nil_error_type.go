package main

// BUG-0045: the facade runtime (stdlib/fork/runtime) does not declare
// runtime.PanicNilError, so a program naming the type does not compile
// under graalgo-tool; the Java-side error type (RuntimeTypes) is not the Go
// one. Found by test/fuzz (corpus/control/panic_values_recovered.go).

import "runtime"

func main() {
	defer func() {
		r := recover()
		e, ok := r.(*runtime.PanicNilError)
		println(ok, e.Error())
	}()
	panic(nil)
}
