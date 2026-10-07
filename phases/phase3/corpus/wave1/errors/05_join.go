package main

import "errors"

// er05: errors.Join — newline-joined formatting, nil filtering,
// all-nil/empty gives nil, single error unwraps to itself.

func main() {
	a := errors.New("first")
	b := errors.New("second")
	// NB: v1 has no variadic argument packing — spread explicitly.
	j := errors.Join([]error{a, nil, b}...)
	println("[" + j.Error() + "]") // "[first\nsecond]"
	// Single non-nil error formats as itself.
	one := errors.Join([]error{nil, a, nil}...)
	println(one.Error())             // "first"
	println(errors.Is(one, a))       // true
	println(errors.Unwrap(one) == nil) // true (Unwrap() []error only)
	// All nil (and empty) Join to nil.
	println(errors.Join([]error{nil, nil}...) == nil) // true
	println(errors.Join([]error{}...) == nil)         // true
	// Joined error Is-matches every member.
	println(errors.Is(j, a)) // true
	println(errors.Is(j, b)) // true
	// Nested joins flatten through the multi-unwrap walk.
	inner := errors.Join([]error{a}...)
	outer := errors.Join([]error{inner, b}...)
	println("[" + outer.Error() + "]")
	println(errors.Is(outer, a)) // true
	println(errors.Is(outer, b)) // true
}
