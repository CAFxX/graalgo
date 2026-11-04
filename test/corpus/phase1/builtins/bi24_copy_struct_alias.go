package main

// copy() must copy struct elements, not alias them: mutating the
// destination's element must not affect the source slice.
type P struct{ F int }

func main() {
	s := []P{{F: 1}, {F: 2}}
	t := make([]P, 2)
	n := copy(t, s)
	t[0].F = 9
	println("n", n, "s0", s[0].F, "t0", t[0].F, "t1", t[1].F)
}
