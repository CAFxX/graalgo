// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): Value.Swap(nil) panics. gc 1.27.1:
// "sync/atomic: swap of nil value into Value" (note: "swap", not
// "store"). Uncaught to stderr; the `panic:` line is compared
// byte-for-byte against gc.
func main() {
	var v atomic.Value
	v.Swap(nil)
}
