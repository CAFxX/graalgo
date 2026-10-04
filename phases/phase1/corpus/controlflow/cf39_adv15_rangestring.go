package main

// R2 adv15 (review remediation): range-over-string capture — the
// RangeStringNode boxing path (cf07 ranges a string without capture;
// cf15 captures a slice var). The rune sum distinguishes per-iteration
// cells (664) from a shared slot (555). gc: 664 5.
func main() {
	var fns [5]func() int
	k := 0
	s := "héllo"
	for _, r := range s {
		fns[k] = func() int { return int(r) }
		k++
	}
	bsum := 0
	for _, f := range fns {
		bsum += f()
	}
	_, _ = bsum, k
}

// observe: bsum k
