// container/ring 01: Link/Unlink semantics, incl. self-link subring
// removal and Link(nil).
package main

import (
	"container/ring"
	"fmt"
)

func fill(r *ring.Ring, base int) {
	p := r
	for i := 0; i < r.Len(); i++ {
		p.Value = base + i
		p = p.Next()
	}
}

func order(r *ring.Ring) []any {
	var out []any
	r.Do(func(v any) { out = append(out, v) })
	return out
}

func main() {
	r1 := ring.New(3)
	fill(r1, 0) // 0 1 2
	r2 := ring.New(2)
	fill(r2, 10) // 10 11

	// Link r2 after r1: combined ring 0 1 2 10 11 (starting at r1).
	oldNext := r1.Link(r2)
	fmt.Println(r1.Len(), r2.Len()) // 5 5
	fmt.Println(order(r1))           // [0 1 2 10 11]
	fmt.Println(oldNext.Value)       // 1 (original r1.Next())

	// Unlink(2) removes r1.Next() and the next one: removes 1,2.
	sub := r1.Unlink(2)
	fmt.Println(r1.Len(), sub.Len()) // 3 2
	fmt.Println(order(r1))            // [0 10 11]
	fmt.Println(order(sub))           // [1 2]

	// Unlink(0) returns nil, ring unchanged.
	fmt.Println(r1.Unlink(0) == nil, r1.Len()) // true 3
	// Unlink(n % len == 0): nothing removed; result is the original next.
	u := r1.Unlink(3)
	fmt.Println(u == nil, r1.Len(), order(r1)) // false 3 [0 10 11]
	_ = sub

	// Link(nil): no-op, returns original next.
	nxt := r1.Link(nil)
	fmt.Println(nxt.Value, r1.Len()) // 10 3

	// Self-link removing a middle segment: r.Link(r.Move(2)) on [0 10 11]
	// removes the element between r and r.Move(2), i.e. 10.
	seg := r1.Link(r1.Move(2))
	fmt.Println(order(r1))  // [0 11]
	fmt.Println(order(seg)) // [10]
}
