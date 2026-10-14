package main

// P0-D1 (review remediation): a closure capturing iteration j's cell, when
// called in a later iteration, must write only iteration j's variable — it
// must NOT corrupt the value that seeds future iterations (gc: 0 1 2 3).
func main() {
	var g func()
	out := 0
	for i := 0; i < 4; i++ {
		if i == 0 {
			g = func() { i += 100 }
		}
		if i == 2 {
			g()
		}
		out = out*10 + i
	}
	_ = out
}

// observe: out
