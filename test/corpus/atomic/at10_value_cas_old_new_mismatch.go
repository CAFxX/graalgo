// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): Value.CompareAndSwap with old/new of different dynamic
// types panics. gc 1.27.1: "sync/atomic: compare and swap of
// inconsistently typed values" (plural "values"). Uncaught to stderr;
// the `panic:` line is compared byte-for-byte against gc.
func main() {
	var v atomic.Value
	v.Store(1)
	v.CompareAndSwap("a", 2)
}
