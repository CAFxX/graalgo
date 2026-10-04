package main

// Loop-capture adversarial: loop breaks early; closures keep the cells
// from the iterations that actually ran.
func main() {
	var fns [5]func() int
	k := 0
	for i := 0; i < 10; i++ {
		x := i * 2
		fns[k] = func() int { return x }
		k++
		if i == 4 {
			break
		}
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
	_ = k
}

// observe: sum k
