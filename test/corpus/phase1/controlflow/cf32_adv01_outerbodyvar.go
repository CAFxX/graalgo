package main

// R2 adv01 (review remediation): inner-loop closure captures the OUTER
// loop's body var — capture propagation must cross two loop levels for
// body vars (cf14 only covers the inner body var). gc: 603 6.
func main() {
	var fns [6]func() int
	k := 0
	for i := 0; i < 3; i++ {
		y := i * 100
		for j := 0; j < 2; j++ {
			fns[k] = func() int { return y + j }
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
