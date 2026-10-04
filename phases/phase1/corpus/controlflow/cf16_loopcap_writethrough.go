package main

// Loop-capture: body writes to the boxed for-header variable propagate to
// the carry cell at end of iteration, so post/cond observe them. The
// capturing funclit forces the header var onto the boxing path — without
// it this test is vacuous (no boxing happens). The closure and the body
// write share the per-iteration cell: iteration 2's funclit observes the
// body write i=10 (sum 0+1+10=11); the post still sees the write (i=11,
// loop ends). gc: 11 3 3.
func main() {
	var fns [5]func() int
	k := 0
	count := 0
	for i := 0; i < 5; i++ {
		fns[k] = func() int { return i }
		k++
		if i == 2 {
			i = 10
		}
		count++
	}
	sum := 0
	for n := 0; n < k; n++ {
		fn := fns[n]
		sum += fn()
	}
	_, _, _ = sum, k, count
}

// observe: sum k count
