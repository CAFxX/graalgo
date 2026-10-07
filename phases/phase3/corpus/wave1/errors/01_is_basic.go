package main

import "errors"

// er01: errors.Is — direct match, miss, and the nil cases.
func main() {
	sentinel := errors.New("sentinel")
	other := errors.New("other")
	println(errors.Is(sentinel, sentinel)) // true
	println(errors.Is(sentinel, other))   // false
	println(errors.Is(other, sentinel))   // false
	// Nil cases: Is(nil, nil) is true; exactly one nil is false.
	var nilErr error
	println(errors.Is(nilErr, nilErr))   // true
	println(errors.Is(nilErr, sentinel)) // false
	println(errors.Is(sentinel, nilErr))  // false
	// Distinct New values with identical text do NOT match.
	println(errors.Is(errors.New("same"), errors.New("same"))) // false
}
