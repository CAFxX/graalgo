package main

import "math/bits"

// Div with y <= hi panics with the runtime overflowError sentinel
// ("runtime error: integer overflow"): the quotient does not fit.
func main() {
	bits.Div64(5, 0, 5)
}
