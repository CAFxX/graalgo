package main

import "errors"

// er03: adversarial errors.Is — uncomparable targets must return false
// (not panic), like gc; multi-error trees; nil Unwrap in the middle.

type sliceErr struct{ xs []int } // uncomparable dynamic type

func (s *sliceErr) Error() string { return "slicerr" }

type wrapErr2 struct {
	msg string
	err error
}

func (w *wrapErr2) Error() string { return w.msg }
func (w *wrapErr2) Unwrap() error { return w.err }

type nilUnwrap struct{}

func (n *nilUnwrap) Error() string { return "nilunwrap" }
func (n *nilUnwrap) Unwrap() error { return nil }

func main() {
	plain := errors.New("plain")
	// Uncomparable target: gc returns false (no panic).
	unc := &sliceErr{xs: []int{1, 2}}
	println(errors.Is(plain, unc)) // false
	println(errors.Is(unc, unc))   // true (identical value)
	// Uncomparable err in a chain, comparable target.
	w := &wrapErr2{msg: "w", err: unc}
	println(errors.Is(w, plain)) // false, no panic
	println(errors.Is(w, unc))    // true
	// Two distinct uncomparable values never match via ==.
	println(errors.Is(&sliceErr{xs: []int{1}}, &sliceErr{xs: []int{1}})) // false

	// nil Unwrap terminates the walk.
	nw := &wrapErr2{msg: "nw", err: &nilUnwrap{}}
	println(errors.Is(nw, plain)) // false

	// Multi-error tree via Join (spread: v1 has no variadic packing).
	a := errors.New("a")
	b := errors.New("b")
	j := errors.Join([]error{a, &wrapErr2{msg: "wb", err: b}}...)
	println(errors.Is(j, a))            // true
	println(errors.Is(j, b))            // true (depth-first into child)
	println(errors.Is(j, errors.New("z"))) // false
}
