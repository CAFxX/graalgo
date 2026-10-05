package helper

import "fmt"

// Greeting is a package-level constant.
const Greeting = "hello"

// Counter is a package-level variable mutated by Inc.
var Counter = 0

// Point is an exported struct type.
type Point struct {
	X int
	Y int
}

// String renders p explicitly (v1 has no Stringer dispatch).
func (p Point) String() string {
	return fmt.Sprintf("(%d,%d)", p.X, p.Y)
}

// Add adds two ints.
func Add(a, b int) int {
	return a + b
}

// Inc bumps Counter and returns the new value.
func Inc() int {
	Counter++
	return Counter
}

// Describe describes a point.
func Describe(p Point) string {
	return "point " + p.String()
}

// Boom panics; exercised by the panic differential variant.
func Boom() {
	panic("helper boom")
}

// T is a struct type designed to collide with main's T (P0-1): helper's
// embedded T must never rebind to main's T during flattening.
type T struct {
	X int
}

// Who identifies which package's T answers.
func (t T) Who() string { return "h.T" }

// Outer embeds T.
type Outer struct {
	T
}
