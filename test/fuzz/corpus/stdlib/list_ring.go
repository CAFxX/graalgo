// Targets container/list (PushFront/Back, InsertBefore/After, Move*,
// PushBackList/PushFrontList incl. self-append, Remove) with elements
// belonging to another list or already removed (must be no-ops), and
// container/ring (New, Link, Unlink, Move with negative/large n, Do, Len).
package main

import (
	"container/list"
	"container/ring"
)

func dump(name string, l *list.List) {
	print(name, " len=", l.Len(), " [")
	for e := l.Front(); e != nil; e = e.Next() {
		print(" ", e.Value.(int))
	}
	print(" ] rev [")
	for e := l.Back(); e != nil; e = e.Prev() {
		print(" ", e.Value.(int))
	}
	println(" ]")
}

func rdump(name string, r *ring.Ring) {
	print(name, " len=", r.Len(), " [")
	r.Do(func(v any) {
		if v == nil {
			print(" nil")
		} else {
			print(" ", v.(int))
		}
	})
	println(" ]")
}

func main() {
	l := list.New()
	for i := 1; i <= 5; i++ {
		l.PushBack(i)
	}
	f := l.PushFront(0)
	dump("init", l)
	e3 := l.Front().Next().Next().Next()
	l.InsertBefore(30, e3)
	l.InsertAfter(31, e3)
	dump("ins", l)
	l.MoveToFront(e3)
	l.MoveToBack(f)
	dump("move", l)
	l.MoveBefore(f, e3)
	l.MoveAfter(e3, l.Back())
	dump("move2", l)
	l.MoveAfter(e3, e3)
	l.MoveBefore(f, f)
	dump("self", l)

	other := list.New()
	o1 := other.PushBack(100)
	other.PushBack(101)
	// operations with a foreign element are no-ops
	l.MoveToFront(o1)
	l.MoveToBack(o1)
	l.MoveBefore(o1, e3)
	l.MoveAfter(e3, o1)
	println("ins-foreign", l.InsertBefore(7, o1) == nil, l.InsertAfter(7, o1) == nil)
	v := l.Remove(o1)
	println("remove-foreign", v.(int), other.Len())
	dump("after-foreign", l)
	dump("other", other)

	rv := l.Remove(e3)
	println("removed", rv.(int), e3.Next() == nil, e3.Prev() == nil)
	println("re-remove", l.Remove(e3).(int))
	l.MoveToFront(e3)
	dump("after-remove", l)

	l.PushBackList(other)
	l.PushFrontList(other)
	dump("lists", l)
	l.PushBackList(l)
	dump("self-back", l)
	l.PushFrontList(l)
	println("self-front len", l.Len())

	var zero list.List
	println("zero", zero.Len(), zero.Front() == nil, zero.Back() == nil)
	zero.PushBack(1)
	zero.PushFront(0)
	dump("zero", &zero)
	l.Init()
	dump("reinit", l)

	// stress: rotate and checksum
	s := list.New()
	for i := 0; i < 1000; i++ {
		s.PushBack(i)
	}
	var x uint32 = 9
	for i := 0; i < 3000; i++ {
		x = x*1664525 + 1013904223
		e := s.Front()
		for k := uint32(0); k < x>>24%5; k++ {
			e = e.Next()
		}
		switch x >> 10 % 4 {
		case 0:
			s.MoveToBack(e)
		case 1:
			s.MoveAfter(e, s.Back().Prev())
		case 2:
			s.Remove(e)
			s.PushBack(e.Value)
		default:
			s.MoveBefore(s.Back(), e)
		}
	}
	var h uint64 = 14695981039346656037
	for e := s.Front(); e != nil; e = e.Next() {
		h = (h ^ uint64(e.Value.(int))) * 1099511628211
	}
	println("stress", s.Len(), h, s.Front().Value.(int), s.Back().Value.(int))

	// ring
	r := ring.New(5)
	for i := 0; i < 5; i++ {
		r.Value = i
		r = r.Next()
	}
	rdump("r", r)
	rdump("move2", r.Move(2))
	rdump("move-7", r.Move(-7))
	rdump("move12", r.Move(12))
	println(r.Prev().Value.(int), r.Next().Value.(int))
	un := r.Unlink(2)
	rdump("after-unlink", r)
	rdump("unlinked", un)
	println("unlink0", r.Unlink(0) == nil, r.Unlink(-1) == nil)
	s2 := ring.New(3)
	for i := 0; i < 3; i++ {
		s2.Value = 10 + i
		s2 = s2.Next()
	}
	ret := r.Link(s2)
	rdump("linked", r)
	rdump("link-ret", ret)
	// link with itself splits
	a := ring.New(6)
	for i := 0; i < 6; i++ {
		a.Value = i
		a = a.Next()
	}
	b := a.Move(3)
	sp := a.Link(b)
	rdump("split-a", a)
	rdump("split-ret", sp)
	var zr ring.Ring
	println("zero-ring", zr.Len(), zr.Next() == &zr)
	rdump("new0", ring.New(0))
	single := ring.New(1)
	single.Value = 42
	rdump("single", single.Move(-100))
	println("unlink-all", single.Unlink(5).Len())
	sum := 0
	big := ring.New(1000)
	for i := 0; i < 1000; i++ {
		big.Value = i * i
		big = big.Move(1)
	}
	big = big.Move(-333)
	big.Unlink(100)
	big.Do(func(v any) { sum += v.(int) % 1000 })
	println("big", big.Len(), sum, big.Value.(int))
}
