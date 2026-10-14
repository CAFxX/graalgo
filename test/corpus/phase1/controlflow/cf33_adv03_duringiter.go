package main

// R2 adv03 (review remediation): closure invoked DURING its iteration —
// pins the refresh prologue seeding the per-iteration cell before the body
// executes. gc: 10 30.
func main() {
	sum := 0
	bsum := 0
	for i := 0; i < 5; i++ {
		f := func() int { return i }
		sum += f()
		x := i * 3
		g := func() int { return x }
		bsum += g()
	}
	_ = sum
	_ = bsum
}

// observe: sum bsum
