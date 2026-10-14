package main

type P struct{ n int }

// R2 adv17 (review remediation): a struct field write to a for-header
// variable in the body must reach post/cond (gc: count=3 last=10).
// The funclit forces per-iteration boxing of p.
func main() {
	count := 0
	last := -1
	for p := (P{0}); p.n < 5; p.n++ {
		f := func() int { return p.n }
		_ = f
		if p.n == 2 {
			p.n = 10
		}
		count++
		last = p.n
	}
	_, _ = count, last
}

// observe: count last
