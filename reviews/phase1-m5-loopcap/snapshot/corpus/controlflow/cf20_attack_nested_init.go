package main

// Self-attack: inner loop init references outer header var; inner funclit
// captures both. Phase stack must resolve outer i to BODY phase.
func main() {
	var fns [6]func() int
	k := 0
	for i := 0; i < 3; i++ {
		for j := i; j < 3; j++ {
			fns[k] = func() int { return i*10 + j }
			k++
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
