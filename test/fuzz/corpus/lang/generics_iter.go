// Targets generic code combined with iterators and closures: generic
// linked list with All() iter.Seq, Map/Filter/Reduce over Seq, generic
// methods on generic types returning Seq2, early termination propagation
// through nested generic adapters, and generic recursion over a tree.
package main

import (
	"iter"
	"strconv"
	"strings"
)

type List[T any] struct {
	head *elem[T]
	size int
}

type elem[T any] struct {
	v    T
	next *elem[T]
}

func (l *List[T]) Push(v T) {
	l.head = &elem[T]{v, l.head}
	l.size++
}

func (l *List[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for e := l.head; e != nil; e = e.next {
			if !yield(e.v) {
				return
			}
		}
	}
}

func (l *List[T]) Enumerate() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		i := 0
		for v := range l.All() {
			if !yield(i, v) {
				return
			}
			i++
		}
	}
}

func Map[A, B any](s iter.Seq[A], f func(A) B) iter.Seq[B] {
	return func(yield func(B) bool) {
		for a := range s {
			if !yield(f(a)) {
				return
			}
		}
	}
}

func Take[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for v := range s {
			if !yield(v) {
				return
			}
			i++
			if i == n {
				return
			}
		}
	}
}

func Reduce[T, A any](s iter.Seq[T], init A, f func(A, T) A) A {
	acc := init
	for v := range s {
		acc = f(acc, v)
	}
	return acc
}

func Naturals(stopped *int) iter.Seq[int] {
	return func(yield func(int) bool) {
		defer func() { *stopped++ }()
		for i := 0; ; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

type Tree[T any] struct {
	Val      T
	Children []*Tree[T]
}

func (t *Tree[T]) Walk(depth int) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		t.walk(depth, yield)
	}
}

func (t *Tree[T]) walk(depth int, yield func(int, T) bool) bool {
	if !yield(depth, t.Val) {
		return false
	}
	for _, c := range t.Children {
		if !c.walk(depth+1, yield) {
			return false
		}
	}
	return true
}

func Depth[T any](t *Tree[T]) int {
	d := 0
	for _, c := range t.Children {
		d = max(d, Depth(c))
	}
	return d + 1
}

type Pt struct{ X, Y int8 }

func main() {
	var l List[Pt]
	for i := range 5 {
		l.Push(Pt{int8(i * 40), int8(-i)})
	}
	for i, p := range l.Enumerate() {
		print(i, "(", p.X, ",", p.Y, ") ")
	}
	println()
	sumX := Reduce(l.All(), int8(0), func(a int8, p Pt) int8 { return a + p.X })
	sumXi := Reduce(l.All(), 0, func(a int, p Pt) int { return a + int(p.X) })
	println(sumX, sumXi, l.size)

	strs := Map(l.All(), func(p Pt) string { return strconv.Itoa(int(p.X)) })
	println(Reduce(strs, "", func(a, s string) string { return a + s + ";" }))

	stopped := 0
	sq := Map(Naturals(&stopped), func(i int) int { return i * i })
	first := []int{}
	for v := range Take(sq, 6) {
		first = append(first, v)
	}
	println(len(first), first[5], stopped)
	for v := range sq {
		if v > 50 {
			break
		}
	}
	println(stopped)
	println(Reduce(Take(Naturals(&stopped), 0), 7, func(a, b int) int { return a + b }), stopped)

	words := Map(Take(Naturals(&stopped), 4), func(i int) []string { return strings.Split(strings.Repeat("w", i+1), "") })
	lens := Reduce(words, "", func(a string, ws []string) string { return a + strconv.Itoa(len(ws)) })
	println(lens, stopped)

	tree := &Tree[string]{Val: "root", Children: []*Tree[string]{
		{Val: "a", Children: []*Tree[string]{{Val: "a1"}, {Val: "a2", Children: []*Tree[string]{{Val: "a2x"}}}}},
		{Val: "b"},
	}}
	for d, v := range tree.Walk(0) {
		print(strings.Repeat(".", d), v, " ")
	}
	println()
	for d, v := range tree.Walk(10) {
		if v == "a2" {
			println("stop at", d)
			break
		}
	}
	println(Depth(tree), Depth(&Tree[int]{}))

	itree := &Tree[[2]int]{Val: [2]int{1, 2}, Children: []*Tree[[2]int]{{Val: [2]int{3, 4}}}}
	total := 0
	for _, v := range itree.Walk(0) {
		v[0] *= 100 // copy
		total += v[0] + v[1]
	}
	println(total, itree.Val[0])

	// panic through generic adapters, recovered
	println(func() (s string) {
		defer func() {
			if r := recover(); r != nil {
				s = "recovered " + r.(string)
			}
		}()
		for v := range Map(Take(Naturals(&stopped), 10), func(i int) int {
			if i == 3 {
				panic("at " + strconv.Itoa(i))
			}
			return i
		}) {
			_ = v
		}
		return "none"
	}(), stopped)
}
