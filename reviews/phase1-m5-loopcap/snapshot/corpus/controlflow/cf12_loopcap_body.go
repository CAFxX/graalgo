package main

// Loop-capture: closures over loop-body variables get per-iteration cells.
func main() {
	var fns [5]func() int
	for i := 0; i < 5; i++ {
		x := i * 10
		fns[i] = func() int { return x }
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
}

// observe: sum
