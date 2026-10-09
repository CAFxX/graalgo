// container/list 00: basic push/remove/iteration, incl. the zero List
// value (lazyInit path) and Front/Back/Len.
package main

import (
	"container/list"
	"fmt"
)

func forward(l *list.List) []any {
	var out []any
	for e := l.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value)
	}
	return out
}

func backward(l *list.List) []any {
	var out []any
	for e := l.Back(); e != nil; e = e.Prev() {
		out = append(out, e.Value)
	}
	return out
}

func main() {
	// Zero List value works without Init (lazyInit).
	z := &list.List{}
	z.PushBack(1)
	z.PushFront(0)
	fmt.Println(z.Len(), z.Front().Value, z.Back().Value) // 2 0 1

	l := list.New()
	fmt.Println(l.Len(), l.Front() == nil, l.Back() == nil) // 0 true true

	e1 := l.PushBack("a")
	e2 := l.PushBack("b")
	e3 := l.PushFront("z")
	fmt.Println(l.Len())      // 3
	fmt.Println(forward(l))   // [z a b]
	fmt.Println(backward(l))  // [b a z]
	fmt.Println(e1.Value, e2.Value, e3.Value)

	// Remove middle, then front, then back.
	fmt.Println(l.Remove(e2)) // b
	fmt.Println(forward(l))   // [z a]
	fmt.Println(l.Remove(e3)) // z
	fmt.Println(l.Remove(e1)) // a
	fmt.Println(l.Len(), l.Front() == nil) // 0 true

	// Removed elements are detached: Next/Prev are nil.
	fmt.Println(e1.Next() == nil, e1.Prev() == nil) // true true

	// Init clears.
	l.PushBack(1)
	l.PushBack(2)
	l.Init()
	fmt.Println(l.Len(), l.Front() == nil) // 0 true
}
