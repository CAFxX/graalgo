package main

// P0-D5 (review remediation): transitive capture through a nested funclit —
// only the inner literal references the loop variable, but the outer
// literal must still bind the iteration that created it (gc: out=1).
func main() {
	var fns [3]func() func() int
	for i := 0; i < 3; i++ {
		fns[i] = func() func() int {
			return func() int { return i }
		}
	}
	mid := fns[1]
	inner := mid()
	out := inner()
	_ = out
}

// observe: out
