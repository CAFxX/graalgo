// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): Value.CompareAndSwap with a new value whose dynamic type
// differs from the stored value's panics. gc 1.27.1: "sync/atomic:
// compare and swap of inconsistently typed value into Value" (singular
// "value into Value"). Uncaught to stderr; the `panic:` line is
// compared byte-for-byte against gc.
func main() {
	var v atomic.Value
	v.Store(1)
	v.CompareAndSwap("a", "b")
}
