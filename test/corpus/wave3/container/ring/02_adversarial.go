// container/ring 02: adversarial — degenerate constructors, nil ring,
// zero Ring value, huge moves, unlink-everything.
package main

import (
	"container/ring"
	"fmt"
)

func main() {
	// Degenerate constructors.
	fmt.Println(ring.New(0) == nil, ring.New(-3) == nil) // true true

	// Nil ring: Len 0, Do no-op.
	var nilr *ring.Ring
	fmt.Println(nilr.Len()) // 0
	called := false
	nilr.Do(func(v any) { called = true })
	fmt.Println(called) // false

	// Zero Ring value: Next/Prev lazily initialize a 1-ring.
	z := &ring.Ring{}
	nxt := z.Next()
	fmt.Println(nxt == z, z.Len()) // true 1
	z.Value = "zv"
	fmt.Println(z.Prev().Value) // zv

	// Huge moves on a small ring.
	r := ring.New(3)
	p := r
	for i := 0; i < 3; i++ {
		p.Value = i
		p = p.Next()
	}
	fmt.Println(r.Move(1000000).Value)  // 1000000 % 3 = 1 -> 1
	fmt.Println(r.Move(-1000001).Value) // -1000001 % 3: -(1000001%3)= -2 -> +1 step back 2 == fwd 1 -> 1

	// Unlink more than len: n % len removed.
	r2 := ring.New(4)
	q := r2
	for i := 0; i < 4; i++ {
		q.Value = i
		q = q.Next()
	}
	sub := r2.Unlink(6) // 6 % 4 = 2 removed
	fmt.Println(r2.Len(), sub.Len()) // 2 2
	var a, b []any
	r2.Do(func(v any) { a = append(a, v) })
	sub.Do(func(v any) { b = append(b, v) })
	fmt.Println(a, b) // [0 3] [1 2]

	// Unlink everything but r itself.
	r3 := ring.New(3)
	s3 := r3
	for i := 0; i < 3; i++ {
		s3.Value = i
		s3 = s3.Next()
	}
	rest := r3.Unlink(2)
	fmt.Println(r3.Len(), rest.Len()) // 1 2
	fmt.Println(r3.Value)             // 0
}
