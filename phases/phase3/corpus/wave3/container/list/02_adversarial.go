// container/list 02: adversarial — remove-from-foreign-list no-op,
// 500-element stress with interleaved removals, insert/remove at
// boundaries, element reuse across lists.
package main

import (
	"container/list"
	"fmt"
)

func vals(l *list.List) []int {
	var out []int
	for e := l.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(int))
	}
	return out
}

func main() {
	// Removing an element that belongs to another list is a no-op
	// returning the value.
	l1 := list.New()
	l2 := list.New()
	e := l1.PushBack("v")
	fmt.Println(l2.Remove(e)) // v
	fmt.Println(l1.Len(), l2.Len()) // 1 0

	// Stress: 500 pushes, remove every 3rd via saved elements, verify.
	l := list.New()
	var elems []*list.Element
	for i := 0; i < 500; i++ {
		elems = append(elems, l.PushBack(i))
	}
	removed := 0
	for i, e := range elems {
		if i%3 == 0 {
			l.Remove(e)
			removed++
		}
	}
	fmt.Println(l.Len(), removed) // 333 167
	got := vals(l)
	ok := len(got) == 333
	for i, v := range got {
		// survivors are the indices not divisible by 3, in order
		want := i + (i/2) + 1
		if v != want {
			ok = false
		}
	}
	fmt.Println(ok) // true

	// Insert at boundaries of a 1-element list.
	s := list.New()
	mid := s.PushBack(10)
	s.InsertBefore(9, mid)
	s.InsertAfter(11, mid)
	fmt.Println(vals(s)) // [9 10 11]

	// MoveBefore/After with adjacent elements (no-op-ish swaps).
	s.MoveAfter(s.Front(), s.Front().Next()) // 9 after 10
	fmt.Println(vals(s)) // [10 9 11]
	s.MoveBefore(s.Back(), s.Front()) // 11 to front
	fmt.Println(vals(s)) // [11 10 9]

	// An element removed then re-pushed belongs to the new list.
	r := list.New()
	re := r.PushBack(7)
	r.Remove(re)
	r2 := list.New()
	r2.PushBack(re.Value)
	fmt.Println(vals(r2), r.Len()) // [7] 0
}
