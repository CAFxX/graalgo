// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): Value.CompareAndSwap with a nil new value panics (only
// the new value is nil-checked; a nil old is legal). gc 1.27.1:
// "sync/atomic: compare and swap of nil value into Value". Uncaught to
// stderr; the `panic:` line is compared byte-for-byte against gc.
func main() {
	var v atomic.Value
	v.CompareAndSwap(1, nil)
}
