// container/ring 00: basic ring construction, Next/Prev/Move, Len, Do.
package main

import (
	"container/ring"
	"fmt"
)

func main() {
	r := ring.New(5)
	// Fill values 0..4 starting at r.
	p := r
	for i := 0; i < 5; i++ {
		p.Value = i
		p = p.Next()
	}
	fmt.Println(r.Len()) // 5

	// Do visits in forward order from r.
	var order []any
	r.Do(func(v any) { order = append(order, v) })
	fmt.Println(order) // [0 1 2 3 4]

	// Move wraps: +7 on a 5-ring == +2; -7 == -2.
	fmt.Println(r.Move(7).Value)  // 2
	fmt.Println(r.Move(-7).Value) // 3
	fmt.Println(r.Move(0) == r)   // true

	// Prev from r is the last element.
	fmt.Println(r.Prev().Value)       // 4
	fmt.Println(r.Next().Value)       // 1
	fmt.Println(r.Prev().Next() == r) // true

	// Single-element ring: Next/Prev are itself.
	one := ring.New(1)
	one.Value = "solo"
	fmt.Println(one.Len())           // 1
	fmt.Println(one.Next() == one)   // true
	fmt.Println(one.Prev() == one)   // true
	fmt.Println(one.Move(100).Value) // solo
}
