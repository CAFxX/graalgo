// container/list 01: Move*/Insert* and Push*List, incl. foreign-mark
// no-ops and self-list copies.
package main

import (
	"container/list"
	"fmt"
)

func vals(l *list.List) []any {
	var out []any
	for e := l.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value)
	}
	return out
}

func main() {
	l := list.New()
	a := l.PushBack("a")
	b := l.PushBack("b")
	c := l.PushBack("c")
	d := l.PushBack("d")

	l.MoveToFront(c)
	fmt.Println(vals(l)) // [c a b d]
	l.MoveToBack(a)
	fmt.Println(vals(l)) // [c b d a]
	l.MoveBefore(d, b)
	fmt.Println(vals(l)) // [c d b a]
	l.MoveAfter(c, a)
	fmt.Println(vals(l)) // [d b a c]

	// Move of an element already in place / e == mark: no-ops.
	l.MoveToFront(d)
	l.MoveBefore(b, b)
	fmt.Println(vals(l)) // [d b a c]

	// InsertBefore/After.
	e := l.InsertBefore("x", b)
	fmt.Println(vals(l)) // [d x b a c]
	l.InsertAfter("y", e)
	fmt.Println(vals(l)) // [d x y b a c]

	// Foreign mark: not modified, nil returned.
	other := list.New()
	other.PushBack("zzz")
	fmt.Println(l.InsertBefore("q", other.Front()) == nil) // true
	fmt.Println(l.InsertAfter("q", other.Front()) == nil)  // true
	fmt.Println(vals(l))                                   // [d x y b a c]

	// Move with a foreign element: no-op.
	l.MoveToFront(other.Front())
	fmt.Println(vals(l)) // [d x y b a c]

	// PushBackList / PushFrontList copies.
	src := list.New()
	src.PushBack(1)
	src.PushBack(2)
	dst := list.New()
	dst.PushBack(0)
	dst.PushBackList(src)
	fmt.Println(vals(dst)) // [0 1 2]
	dst.PushFrontList(src)
	fmt.Println(vals(dst)) // [1 2 0 1 2]
	fmt.Println(vals(src)) // [1 2] (source untouched)

	// Self PushBackList duplicates.
	self := list.New()
	self.PushBack("s")
	self.PushBackList(self)
	fmt.Println(vals(self), self.Len()) // [s s] 2

	_ = a
}
