// Traceback of a run-time error raised by an operation, not a panic call
// (WP-TR-AUDIT-6): the faulting statement's line in the innermost frame, the
// call sites' lines above it, and a deferred call that runs first without
// recovering. Compared with frames by tbnorm.py.
// m1a:traceback
package main

var table = []int{1, 2, 3}

func index(i int) int {
	x := 0
	x += table[i]
	return x
}

func sum(n int) int {
	s := 0
	for i := 0; i <= n; i++ {
		s += index(i)
	}
	return s
}

func main() {
	defer println("deferred ran")
	println(sum(2))
	println(sum(3))
}
