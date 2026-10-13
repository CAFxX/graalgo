package main

import "math/bits"

// Div by zero panics with the runtime divideError sentinel
// ("runtime error: integer divide by zero").
func main() {
	bits.Div(0, 1, 0)
}
