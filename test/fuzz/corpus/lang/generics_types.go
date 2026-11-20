// Targets generic types: methods on generic types, recursive Node[T],
// F-bounded constraints (T interface{ Less(T) bool }), embedded generic
// types in structs, generic aliases (go1.24+), instantiation with struct,
// array, pointer, interface and func types, and zero values var z T.
package main

import (
	"strconv"
	"strings"
)

type Node[T any] struct {
	Val         T
	Left, Right *Node[T]
}

type Lesser[T any] interface{ Less(T) bool }

func Insert[T Lesser[T]](n *Node[T], v T) *Node[T] {
	if n == nil {
		return &Node[T]{Val: v}
	}
	if v.Less(n.Val) {
		n.Left = Insert(n.Left, v)
	} else {
		n.Right = Insert(n.Right, v)
	}
	return n
}

func Walk[T any](n *Node[T], f func(T)) {
	if n == nil {
		return
	}
	Walk(n.Left, f)
	f(n.Val)
	Walk(n.Right, f)
}

type Age int

func (a Age) Less(b Age) bool { return a < b }

type Pt struct{ X, Y int }

func (p Pt) Less(q Pt) bool { return p.X < q.X || (p.X == q.X && p.Y < q.Y) }

type Rev string

func (r Rev) Less(s Rev) bool { return r > s }

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}
func (s Stack[T]) Len() int { return len(s.items) }

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Swap() Pair[string, K] {
	return Pair[string, K]{Key: "swapped", Val: p.Key}
}

// embedded generic types
type Named struct {
	Stack[string]
	Pair[int, [2]float64]
	Name string
}

type List[T any] = []T                 // generic alias
type PairOf[T comparable] = Pair[T, T] // generic alias to generic type
type IntStack = Stack[int]

type Box[T any] struct{ V T }

func (b Box[T]) Get() T   { return b.V }
func (b *Box[T]) Set(v T) { b.V = v }
func Zero[T any]() (z T)  { return }
func IsZero[T comparable](v T) bool {
	var z T
	return v == z
}

type Shower interface{ Show() string }

func (p Pt) Show() string { return "(" + strconv.Itoa(p.X) + "," + strconv.Itoa(p.Y) + ")" }

func ShowAll[T Shower](xs []T) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = x.Show()
	}
	return strings.Join(parts, " ")
}

func main() {
	var root *Node[Age]
	for _, v := range []Age{5, 3, 8, 1, 4, 9, 7} {
		root = Insert(root, v)
	}
	Walk(root, func(a Age) { print(int(a), " ") })
	println()

	var pr *Node[Pt]
	for _, p := range []Pt{{2, 1}, {1, 5}, {2, 0}, {1, 1}} {
		pr = Insert(pr, p)
	}
	Walk(pr, func(p Pt) { print(p.Show(), " ") })
	println()

	var rr *Node[Rev]
	for _, s := range strings.Fields("m c x a q") {
		rr = Insert(rr, Rev(s))
	}
	Walk(rr, func(r Rev) { print(string(r)) })
	println()

	var si Stack[[3]int]
	si.Push([3]int{1, 2, 3})
	a := [3]int{4, 5, 6}
	si.Push(a)
	a[0] = 99 // must not affect stored copy
	top, ok := si.Pop()
	println(top[0], top[2], ok, si.Len())
	si.Pop()
	z, ok := si.Pop()
	println(z[0], z[1], z[2], ok)

	var sp Stack[*Pt]
	p := &Pt{1, 1}
	sp.Push(p)
	p.X = 50
	got, _ := sp.Pop()
	println(got.X, got == p)
	nilp, ok := sp.Pop()
	println(nilp == nil, ok)

	var sf Stack[func() int]
	for i := range 3 {
		sf.Push(func() int { return i * 7 })
	}
	f1, _ := sf.Pop()
	f2, _ := sf.Pop()
	println(f1(), f2(), sf.Len())

	var sa Stack[any]
	sa.Push(1)
	sa.Push("s")
	sa.Push(Pt{1, 2})
	sa.Push(nil)
	for sa.Len() > 0 {
		v, _ := sa.Pop()
		switch x := v.(type) {
		case nil:
			print("nil ")
		case int:
			print("int", x, " ")
		case string:
			print("str", x, " ")
		case Pt:
			print("pt", x.Show(), " ")
		}
	}
	println()

	n := Named{Name: "n"}
	n.Push("a")
	n.Push("b")
	n.Key = 7
	n.Val[1] = 2.5
	sw := n.Swap()
	println(n.Len(), n.Key, sw.Key, sw.Val, n.Pair.Val[1] == 2.5)
	n2 := n
	n2.Val[1] = 9
	n2.Push("c") // may share backing array but n's len unchanged
	println(n.Len(), n2.Len(), n.Val[1] == 2.5)

	var l List[Pt] = []Pt{{3, 4}}
	l = append(l, Pt{5, 6})
	println(len(l), ShowAll(l))
	po := PairOf[string]{Key: "k", Val: "v"}
	println(po.Key, po.Val, po.Swap().Val)
	var is IntStack
	is.Push(42)
	v, _ := is.Pop()
	println(v)

	b := Box[[2]string]{V: [2]string{"x", "y"}}
	g := b.Get
	b.Set([2]string{"p", "q"})
	println(g()[0], b.Get()[1])
	bi := Box[any]{}
	println(bi.Get() == nil)
	bi.Set(Box[int]{V: 3})
	println(bi.Get().(Box[int]).V)

	println(Zero[int](), Zero[string]() == "", Zero[*Pt]() == nil, Zero[[2]int]()[1], Zero[Pt]().X, Zero[any]() == nil, Zero[func()]() == nil)
	println(IsZero(0), IsZero(Pt{}), IsZero(Pt{0, 1}), IsZero([2]string{}), IsZero[any](nil), IsZero[any](0), IsZero[*int](nil))
	println(IsZero(Box[Pt]{}), IsZero(Pair[int, string]{}), IsZero(Pair[int, string]{Val: "x"}))
}
