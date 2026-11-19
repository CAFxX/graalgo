// Targets the unrecovered panic(nil) printout under go1.21+ semantics:
// gc wraps it in *runtime.PanicNilError and prints
// "panic: runtime error: panic called with nil argument"; exit 2.
package main

import "runtime"

func tryNil() (ok bool) {
	defer func() {
		_, ok = recover().(*runtime.PanicNilError)
	}()
	panic(nil)
}

func main() {
	println("recovered PanicNilError:", tryNil())
	var err error
	defer println("deferred before fatal")
	panic(err)
}
