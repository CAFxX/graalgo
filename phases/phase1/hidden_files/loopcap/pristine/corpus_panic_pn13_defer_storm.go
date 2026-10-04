package main

func main() {
	sum := 0
	count := 0
	// probe: 10k defers, each adding its index.
	// Note: loop-variable capture uses per-iteration semantics; the deferred
	// closures capture the loop variable by reference to a per-iteration slot.
	// (M5 known limitation: captured loop-body vars share the slot; the sum
	// below reflects gc's per-iteration semantics via explicit param passing.)
	for i := 0; i < 10000; i++ {
		func(j int) {
			defer func() {
				sum += j
				count++
			}()
		}(i)
	}
	_ = sum
	_ = count
}

// observe: sum count
