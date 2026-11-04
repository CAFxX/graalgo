package main

// Append with growth must copy old struct elements, not alias them:
// mutating the grown slice's element must not affect the old slice.
type P struct{ F int }

func main() {
	s := make([]P, 1, 1)
	s[0].F = 1
	s2 := append(s, P{F: 2})
	s2[0].F = 9
	println("s0", s[0].F, "s2_0", s2[0].F, "s2_1", s2[1].F)
}
