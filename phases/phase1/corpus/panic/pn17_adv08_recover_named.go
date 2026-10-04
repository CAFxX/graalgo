package main

// R2 adv08 (review remediation): recover in a defer, then set the named
// result — the canonical panic-to-value translation. Not covered by pn03
// (no panic) or pn07 (no named-result write after recover). gc: 99.
func main() {
	f := func() (r int) {
		defer func() {
			if recover() != nil {
				r = 99
			}
		}()
		panic("x")
	}
	got := f()
	// probe
	_ = got
}

// observe: got
