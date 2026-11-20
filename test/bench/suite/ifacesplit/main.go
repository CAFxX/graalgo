// Interface dispatch in a shared helper (SPEC §2.6.1): total ranges over
// []Shape and calls Area; five call sites each pass a slice of one dynamic
// type, five types in all. The helper's one call site is megamorphic unless
// the runtime gives each caller its own copy (Truffle splitting); gc
// dispatches through the itab at every element.
package main

import "graalgo/bench/h"

type Shape interface{ Area() int }

type A struct{ x int }
type B struct{ x int }
type C struct{ x int }
type D struct{ x int }
type E struct{ x int }

func (a A) Area() int { return a.x + 1 }
func (b B) Area() int { return b.x * 2 }
func (c C) Area() int { return c.x ^ 3 }
func (d D) Area() int { return d.x - 4 }
func (e E) Area() int { return e.x << 1 }

func total(xs []Shape) int {
	s := 0
	for _, x := range xs {
		s += x.Area()
	}
	return s
}

func mk(n int, f func(int) Shape) []Shape {
	r := make([]Shape, n)
	for i := range r {
		r[i] = f(i)
	}
	return r
}

var a, b, c, d, e = mk(1000, func(i int) Shape { return A{i} }), mk(1000, func(i int) Shape { return B{i} }),
	mk(1000, func(i int) Shape { return C{i} }), mk(1000, func(i int) Shape { return D{i} }),
	mk(1000, func(i int) Shape { return E{i} })

func main() {
	h.Run("ifacesplit", 30, func() int {
		t := 0
		for k := 0; k < 10; k++ {
			t += total(a) + total(b) + total(c) + total(d) + total(e)
		}
		return t
	})
}
