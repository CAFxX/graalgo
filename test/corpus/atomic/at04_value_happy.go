// verifies: GG-0020
package main

import "sync/atomic"

// P1-6 (RV22): atomic.Value happy paths and gc-defined non-panicking
// edges. A failed check panics; success is silent with exit 0 (both
// runtimes). Covers: Store/Load/Swap/CompareAndSwap round-trips,
// CAS(nil, x) on an empty Value (stores, reports true — gc does NOT
// panic), and typed-nil stores (a non-nil interface, always legal).
func check(cond bool, msg string) {
	if !cond {
		panic("check failed: " + msg)
	}
}

func main() {
	var v atomic.Value
	v.Store(42)
	check(v.Load() == 42, "load after store")
	check(v.Swap(43) == 42, "swap returns old")
	check(v.Load() == 43, "load after swap")
	check(v.CompareAndSwap(43, 44), "cas true")
	check(v.Load() == 44, "load after cas")
	check(!v.CompareAndSwap(43, 45), "cas false")
	check(v.Load() == 44, "load unchanged after failed cas")

	var e atomic.Value
	check(e.CompareAndSwap(nil, 7), "cas nil-old on empty stores")
	check(e.Load() == 7, "load after cas-nil-old")
	check(!e.CompareAndSwap(99, 8), "cas stale old")
	check(e.Load() == 7, "load unchanged after stale cas")

	var p *int
	var t atomic.Value
	t.Store(p)
	check(t.Load() != nil, "typed nil is a legal non-nil interface")

	u := new(atomic.Value)
	u.Store("s")
	check(u.Load() == "s", "new() form")

	w := atomic.Value{}
	w.Store(1.5)
	check(w.Swap(2.5) == 1.5, "literal form")
}
