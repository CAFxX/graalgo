package main

// R2 adv09 (review remediation; int-log adaptation — the panic corpus
// observes ints): defer registered DURING unwinding runs before the
// remaining earlier defers; re-panic inside replaces the in-flight panic.
// Trace: p1 → d3(1) → d2(2, panic p2) → inner defer(3) → d1 recovers(4).
// gc: 1234.
func main() {
	wrap := func() (r int) {
		defer func() {
			if recover() != nil {
				r = r*10 + 4
			}
		}()
		defer func() {
			defer func() { r = r*10 + 3 }()
			r = r*10 + 2
			panic("p2")
		}()
		defer func() { r = r*10 + 1 }()
		panic("p1")
	}
	got := wrap()
	// probe
	_ = got
	_ = wrap
}

// observe: got
