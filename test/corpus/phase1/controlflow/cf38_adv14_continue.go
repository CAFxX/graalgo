package main

// R2 adv14 (review remediation): continue with capture — closures keep
// cells only from iterations that ran the capture point (cf19 covers
// break; continue untested). gc: 32 4.
func main() {
	var fns [5]func() int
	k := 0
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			continue
		}
		x := i * 2
		fns[k] = func() int { return x }
		k++
		if i == 7 {
			break
		}
	}
	sum := 0
	for n := 0; n < k; n++ {
		fn := fns[n]
		sum += fn()
	}
	_, _ = sum, k
}

// observe: sum k
