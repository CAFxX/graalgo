package main

// P0-D4 (review remediation): `for i = range ...` (`=` form) into an
// enclosing for-header variable assigns the per-iteration variable
// (gc: sum=11).
func main() {
	sum := 0
	s := []int{7, 8}
	for i := 0; i < 2; i++ {
		f := func() int { return i }
		_ = f
		for i = range s {
			sum = sum*10 + i
		}
		sum = sum*10 + i
	}
	_ = sum
}

// observe: sum
