package main

type Number interface {
	~int | ~int8 | ~uint16 | ~float64
}

type Stack[T any] struct{ items []T }

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

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Flip() Pair[K, K] { return Pair[K, K]{p.Key, p.Key} }

func Sum[T Number](xs ...T) (s T) {
	for _, x := range xs {
		s += x
	}
	return
}

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func Keys[K comparable, V any](m map[K]V) int {
	n := 0
	for range m {
		n++
	}
	return n
}

type Stringer interface{ String() string }

type ID int

func (i ID) String() string { return "id" }

func Join[T Stringer](xs []T) (s string) {
	for _, x := range xs {
		s += x.String()
	}
	return
}

func Index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

type small int8

type Tree[T any] struct {
	Left, Right *Tree[T]
	Val         T
}

func (t *Tree[T]) Walk(f func(T)) {
	if t == nil {
		return
	}
	t.Left.Walk(f)
	f(t.Val)
	t.Right.Walk(f)
}

func main() {
	var s Stack[string]
	s.Push("a")
	s.Push("b")
	v, ok := s.Pop()
	var si Stack[Pair[int, float64]]
	si.Push(Pair[int, float64]{1, 2.5})
	p, _ := si.Pop()
	_, empty := si.Pop()
	println(v, ok, p.Key, p.Val, p.Flip().Val, empty)

	println(Sum(1, 2, 3), Sum(1.5, 2.5), Sum[small](100, 100), Sum[uint16](65535, 2), Sum[int]())
	strs := Map([]int{1, 2, 3}, func(i int) string { return "x" })
	lens := Map(strs, func(s string) int { return len(s) })
	println(len(strs), lens[2], Keys(map[string]bool{"a": true, "b": false}))
	println(Join([]ID{1, 2}), Index([]string{"a", "b"}, "b"), Index([]float64{1, 2}, 3),
		Index([]Pair[int, string]{{1, "a"}, {2, "b"}}, Pair[int, string]{2, "b"}))

	t := &Tree[int]{Val: 2, Left: &Tree[int]{Val: 1}, Right: &Tree[int]{Val: 3}}
	acc := 0
	t.Walk(func(v int) { acc = acc*10 + v })
	println(acc)
}
