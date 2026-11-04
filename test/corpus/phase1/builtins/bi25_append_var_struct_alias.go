package main

// Appending a struct variable must copy it, not alias the variable:
// mutating the variable afterwards must not affect the slice element.
type P struct{ F int }

func main() {
	v := P{F: 1}
	s := append([]P{}, v)
	v.F = 9
	println("v", v.F, "s0", s[0].F)
}
