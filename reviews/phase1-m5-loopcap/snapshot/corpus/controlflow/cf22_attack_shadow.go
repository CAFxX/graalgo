package main

// Self-attack: inner loop var shadows outer; funclit must capture the
// inner (different oid), not the outer.
func main() {
	var fns [6]func() int
	k := 0
	for i := 0; i < 3; i++ {
		for i := 0; i < 2; i++ {
			fns[k] = func() int { return i }
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
