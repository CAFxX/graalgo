package main

// P0-D3 (review remediation): a closure created in the post statement
// captures the per-iteration variable (gc: g observes iteration 1, out=1).
func main() {
	var g func() int
	set := false
	setG := func(f func() int) int {
		if !set {
			g = f
			set = true
		}
		return 0
	}
	out := -1
	for i := 0; i < 3; i, _ = i+1, setG(func() int { return i }) {
		if i == 2 {
			out = g()
		}
	}
	_ = out
}

// observe: out
