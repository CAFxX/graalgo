package main

// Generics: constraints, inference, explicit instantiation, generic types
// and methods, local types per instantiation, type switches on type
// parameters, constraint type sets.

type Number interface {
	~int | ~int8 | ~float64
}

type MyInt int

func (m MyInt) String() string { return "MyInt" }

func Sum[T Number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
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

func (s Stack[T]) Fold[U any](init U, f func(U, T) U) U {
	acc := init
	for _, x := range s.items {
		acc = f(acc, x)
	}
	return acc
}

type Pair[K comparable, V any] struct {
	K K
	V V
}

func Local[X any]() any {
	type T struct{ a int }
	return T{1}
}

func Kind[T any](v T) string {
	switch any(v).(type) {
	case int:
		return "int"
	case string:
		return "string"
	case []T:
		return "slice of T"
	}
	return "other"
}

func Conv[T ~int64 | ~float64](x int) T { return T(1<<40) + T(x) }

type Stringer interface{ String() string }

func Str[T Stringer](v T) string { return v.String() }

func Keys[M ~map[K]V, K comparable, V any](m M) int {
	n := 0
	for range m {
		n++
	}
	return n
}

type List[T any] struct {
	next *List[T]
	val  T
}

func (l *List[T]) Len() int {
	if l == nil {
		return 0
	}
	return 1 + l.next.Len()
}

func main() {
	println(Sum(1, 2, 3), Sum[float64](1.5, 2), Sum[MyInt](4, 5), Sum[int8](100, 100))
	strs := Map([]int{1, 2, 3}, func(i int) string { return string(rune('a' + i)) })
	println(len(strs), strs[0]+strs[1]+strs[2])
	var st Stack[string]
	st.Push("x")
	st.Push("y")
	v, ok := st.Pop()
	println(v, ok, len(st.items))
	var is Stack[int]
	is.Push(4)
	is.Push(5)
	println(is.Fold[int](1, func(a, b int) int { return a * b }), Stack[int].Fold[string](is, "", func(a string, b int) string { return a + "#" }))
	p := Pair[string, int]{"k", 1}
	q := Pair[string, int]{"k", 1}
	println(p == q, p.K, any(p) == any(q))
	println(Local[int]() == Local[string](), Local[int]() == Local[int]())
	println(Kind(1), Kind("s"), Kind([]float64{}), Kind(2.5))
	println(Conv[int64](3), Conv[float64](3))
	println(Str(MyInt(3)))
	println(Keys(map[string]bool{"a": true, "b": false}))
	l := &List[int]{val: 1, next: &List[int]{val: 2}}
	println(l.Len())
	var e any = Pair[int, int]{1, 2}
	_, isPair := e.(Pair[int, int])
	_, isOther := e.(Pair[int, string])
	println(isPair, isOther)
}
