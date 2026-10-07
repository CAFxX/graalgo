package main

import "errors"

// er04: errors.Unwrap — single unwrap, nil for non-wrappers, and the
// Join rule: Unwrap does NOT unwrap Unwrap() []error results.

type wrapErr3 struct{ err error }

func (w *wrapErr3) Error() string { return "w3" }
func (w *wrapErr3) Unwrap() error { return w.err }

func main() {
	inner := errors.New("inner")
	w := &wrapErr3{err: inner}
	u := errors.Unwrap(w)
	println(u == inner) // true
	println(errors.Unwrap(inner) == nil) // true: no Unwrap method
	var nilErr error
	println(errors.Unwrap(nilErr) == nil) // true

	// Join errors implement Unwrap() []error only: Unwrap returns nil.
	j := errors.Join([]error{inner, errors.New("second")}...)
	println(errors.Unwrap(j) == nil) // true

	// Nested: unwrap twice to reach the sentinel.
	w2 := &wrapErr3{err: w}
	println(errors.Unwrap(errors.Unwrap(w2)) == inner) // true
}
