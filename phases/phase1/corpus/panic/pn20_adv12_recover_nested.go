package main

// R2 adv12 (review remediation): recover() must be called DIRECTLY by a
// deferred function — a nested call inside the deferred function observes
// no panic in flight (nil). Not covered by pn08 (top-level and defer-arg
// positions only). gc: 1.
func main() {
	wrap := func() (r int) {
		defer func() { _ = recover() }()
		defer func() {
			func() {
				if recover() == nil {
					r = 1
				} else {
					r = 2
				}
			}()
		}()
		panic("x")
	}
	got := wrap()
	// probe
	_ = got
	_ = wrap
}

// observe: got
