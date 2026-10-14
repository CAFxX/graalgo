package main

type P struct{ n int }

// R2 adv16 (review remediation): struct-typed for-header variable — post
// must read and write the per-iteration cell (gc: sum=3 k=3).
func main() {
	var fns [3]func() int
	k := 0
	sum := 0
	for p := (P{0}); p.n < 3; p.n++ {
		fns[k] = func() int { return p.n }
		k++
	}
	for _, f := range fns {
		sum += f()
	}
	_, _ = sum, k
}

// observe: sum k
