package main

// Self-attack: closures modify their captured per-iteration cell; each
// sees its own cell, not the others'.
func main() {
	var fns [5]func() int
	for i := 0; i < 5; i++ {
		x := i
		fns[i] = func() int {
			x = x*10 + 1
			return x
		}
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
}

// observe: sum
