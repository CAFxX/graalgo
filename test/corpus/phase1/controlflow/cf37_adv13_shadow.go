package main

// R2 adv13 (review remediation): body var shadows the for-header var —
// the closure must capture the inner oid (per-iteration), not the outer
// header oid (capturing the outer oid would give 9). gc: 303 3.
func main() {
	var fns [3]func() int
	k := 0
	for i := 0; i < 3; i++ {
		i := 100 + i
		fns[k] = func() int { return i }
		k++
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_, _ = sum, k
}

// observe: sum k
