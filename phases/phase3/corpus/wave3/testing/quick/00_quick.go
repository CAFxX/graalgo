// 00_quick: Check (pass and deterministic fail), CheckEqual, SetupError,
// and value generation across types. Only pass/fail booleans and counts are
// printed: generated values differ between implementations by design.
package main

import (
	"fmt"
	"testing/quick"
)

func main() {
	// Passing property.
	f := func(x int, y int) bool { return x+y == y+x }
	err := quick.Check(f, &quick.Config{MaxCount: 50})
	fmt.Println("check-pass:", err == nil)

	// Deterministically failing property (fails on input #1 in both legs).
	err = quick.Check(func(x int) bool { return false }, &quick.Config{MaxCount: 10})
	if ce, ok := err.(*quick.CheckError); ok {
		fmt.Println("check-fail: count =", ce.Count, "inputs =", len(ce.In))
	} else {
		fmt.Println("BAD: expected *CheckError, got", err)
	}

	// CheckEqual: f != g.
	err = quick.CheckEqual(
		func(x int) int { return x + 1 },
		func(x int) int { return x + 2 },
		&quick.Config{MaxCount: 10},
	)
	fmt.Println("checkequal-fail:", err != nil)

	// CheckEqual: f == g passes.
	err = quick.CheckEqual(
		func(x string) string { return x },
		func(x string) string { return x },
		&quick.Config{MaxCount: 20},
	)
	fmt.Println("checkequal-pass:", err == nil)

	// SetupError: non-function.
	err = quick.Check(42, nil)
	fmt.Println("setup-err:", err)

	// SetupError: wrong signature (non-bool return).
	err = quick.Check(func(x int) int { return x }, nil)
	fmt.Println("sig-err:", err)

	// Multi-arg generation incl. floats, bools, strings, runes, uints.
	// (Property is unconditional: generated value domains differ
	// between implementations by design.)
	g := func(a bool, b float64, c string, d rune, e uint8) bool {
		_ = a
		_ = b
		_ = d
		_ = e
		return len(c) >= 0
	}
	err = quick.Check(g, &quick.Config{MaxCount: 30})
	fmt.Println("multi-pass:", err == nil)

	// Nil config uses defaults.
	err = quick.Check(func(x int) bool { return true }, nil)
	fmt.Println("nil-config-pass:", err == nil)

	// MaxCountScale multiplies the count (fail on input 1 still).
	cfg := &quick.Config{MaxCount: 5, MaxCountScale: 3}
	err = quick.Check(func(x int) bool { return false }, cfg)
	if ce, ok := err.(*quick.CheckError); ok {
		fmt.Println("scaled count:", ce.Count)
	}
}
