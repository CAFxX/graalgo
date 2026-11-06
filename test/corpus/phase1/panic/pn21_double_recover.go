package main

// B-P1-5 (review remediation): recover() returns the panic value only on
// the first call in a deferred function — gc returns nil on the second
// call in the same defer, and a later (earlier-registered) deferred
// function also sees nil after recovery. gc: 1 1 1.
func main() {
	first := 0
	second := 0
	later := 0
	// probe
	defer func() {
		if recover() == nil {
			later = 1
		}
	}()
	defer func() {
		if recover() != nil {
			first = 1
		}
		if recover() == nil {
			second = 1
		}
	}()
	panic("x")
	_ = first
	_ = second
	_ = later
}

// observe: first second later
