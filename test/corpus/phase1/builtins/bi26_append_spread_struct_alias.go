package main

// append(s, t...) must copy t's struct elements, not alias them:
// mutating the result's element must not affect the spread source.
type P struct{ F int }

func main() {
	t := []P{{F: 1}, {F: 2}}
	s := []P{}
	s2 := append(s, t...)
	s2[0].F = 9
	println("t0", t[0].F, "s2_0", s2[0].F, "s2_1", s2[1].F)
}
