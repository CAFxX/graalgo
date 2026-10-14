package main

// R2 adv10 (review remediation; fmt-free adaptation — the controlflow
// corpus has no imports): boxed loop vars passed through call boundaries.
// In-body reads and captured reads must both see through the GoBox at the
// call boundary. gc: 33 33.
func main() {
	id := func(x int) int { return x }
	var fns [3]func() int
	s := 0
	for i := 0; i < 3; i++ {
		x := i * 11
		fns[i] = func() int { return id(x) }
		s += id(x)
	}
	t := 0
	for _, f := range fns {
		t += f()
	}
	_, _ = s, t
}

// observe: s t
