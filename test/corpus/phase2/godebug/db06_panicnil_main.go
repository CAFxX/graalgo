package main

// Probe for the standard launcher (graalgo.launcher.Main) GODEBUG hook:
// with GODEBUG=panicnil=1, recover() after panic(nil) must return nil
// (pre-1.21 semantics); by default it returns a non-nil *PanicNilError.
// Uses builtin println (stderr, like gc) — the phase-1 observable channel.
func main() {
	wasNil := true
	func() {
		defer func() {
			r := recover()
			wasNil = r == nil
		}()
		panic(nil)
	}()
	if wasNil {
		println("recovered nil")
	} else {
		println("recovered non-nil")
	}
	println("after")
}
