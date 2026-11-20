package main

// atomic.Value.CompareAndSwap reads the stored interface's data word and
// CASes that same word: the data word of a boxed scalar (string, int) must
// be one pointer for the value's life, as gc's is (BUG-0045, found by
// test/fuzz). Struct and pointer values are their own words.

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
