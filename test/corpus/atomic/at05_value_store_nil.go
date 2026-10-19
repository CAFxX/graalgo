// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): Value.Store(nil) panics. gc 1.27.1 (sync/atomic/value.go):
// "sync/atomic: store of nil value into Value". The panic goes uncaught
// to stderr; the differential harness normalizes the trace and compares
// the `panic:` line byte-for-byte against gc.
func main() {
	var v atomic.Value
	v.Store(nil)
}
