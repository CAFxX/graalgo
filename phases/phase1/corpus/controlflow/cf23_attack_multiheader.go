package main

// Self-attack: multiple for-header variables, both captured.
func main() {
	var fns [3]func() int
	k := 0
	for i, j := 0, 10; i < 3; i, j = i+1, j-1 {
		fns[k] = func() int { return i*100 + j }
		k++
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
	_ = k
}

// observe: sum k
