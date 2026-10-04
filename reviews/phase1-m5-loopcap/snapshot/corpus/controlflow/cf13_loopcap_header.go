package main

// Loop-capture: body closures over the for-header variable see per-iteration
// values (go1.22+ semantics).
func main() {
	var fns [5]func() int
	for i := 0; i < 5; i++ {
		fns[i] = func() int { return i }
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
}

// observe: sum
