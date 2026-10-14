package main

type T struct{ x int }

// P0-D2 (review remediation): a struct field mutation in the body must be
// visible to post/cond — they share the one per-iteration variable (gc:
// the loop runs once, n=1).
func main() {
	var sink func() int
	assigned := false
	n := 0
	for s := (T{x: 0}); s.x < 3; s.x++ {
		s.x = 5
		if !assigned {
			sink = func() int { return s.x }
			assigned = true
		}
		n++
	}
	_ = n
	_ = sink
}

// observe: n
