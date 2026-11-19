package main

// BUG-0044: atomic.Value.CompareAndSwap compares the current value with
// old through an interface rebuilt word by word (efaceWords); under graalgo
// the comparison fails for equal values, so CAS reports false and does not
// swap. Found by test/fuzz (corpus/control/atomic_types.go).

import "sync/atomic"

type pt struct{ x, y int }

func main() {
	var s atomic.Value
	s.Store("first")
	println("string", s.CompareAndSwap("first", "second"), s.Load().(string))
	var i atomic.Value
	i.Store(7)
	println("int", i.CompareAndSwap(7, 8), i.Load().(int))
	var p atomic.Value
	p.Store(pt{1, 2})
	println("struct", p.CompareAndSwap(pt{1, 2}, pt{3, 4}), p.Load().(pt).x)
	q := &pt{}
	var r atomic.Value
	r.Store(q)
	println("pointer", r.CompareAndSwap(q, &pt{5, 6}), r.Load().(*pt).x)
}
