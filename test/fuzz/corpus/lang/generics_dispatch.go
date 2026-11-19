// Targets generic dispatch: type switch on any(v).(type) inside generic
// functions, comparable with interfaces panicking at runtime on
// uncomparable dynamic types, type sets with methods, len of generic array
// type params, generic closures, and generic methods returning closures.
package main

import (
	"cmp"
	"slices"
	"strconv"
)

func Kind[T any](v T) string {
	switch x := any(v).(type) {
	case nil:
		return "nil"
	case int:
		return "int:" + strconv.Itoa(x)
	case int8, uint8:
		return "byteish"
	case string:
		return "string:" + x
	case []T:
		return "slice-of-T"
	case *T:
		return "ptr-to-T"
	case error:
		return "error"
	case interface{ Len() int }:
		return "lenner:" + strconv.Itoa(x.Len())
	default:
		return "other"
	}
}

func KindPtr[T any](v T) string { return Kind(&v) }

func Eq[T comparable](a, b T) (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = "panic:" + r.(interface{ Error() string }).Error()
		}
	}()
	if a == b {
		return "eq"
	}
	return "ne"
}

func Index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

type Lenner interface {
	~[4]int | ~[8]int
	Len() int
}

type A4 [4]int
type A8 [8]int

func (a A4) Len() int { return len(a) * 100 }
func (a A8) Len() int { return len(a) * 100 }

func Lens[T Lenner](v T) (int, int) { return len(v), v.Len() }

func ArrLen[T ~[3]string | ~[5]string](v T) int {
	n := 0
	for i := 0; i < len(v); i++ {
		n += i + len(v[i])
	}
	return len(v)*10 + n
}

func SumFirst[T ~[4]int | ~[8]int](v T) int {
	s := 0
	for i := 0; i < len(v); i++ {
		s += v[i]
	}
	v[0] = 1000 // copy
	return s + v[0]
}

func Memo[K comparable, V any](f func(K) V) (func(K) V, func() int) {
	cache := map[K]V{}
	hits := 0
	return func(k K) V {
			if v, ok := cache[k]; ok {
				hits++
				return v
			}
			v := f(k)
			cache[k] = v
			return v
		}, func() int {
			return hits
		}
}

func Compose[A, B, C any](f func(A) B, g func(B) C) func(A) C {
	return func(a A) C { return g(f(a)) }
}

type Ord[T cmp.Ordered] struct{ vals []T }

func (o *Ord[T]) Add(v T) *Ord[T] { o.vals = append(o.vals, v); return o }
func (o *Ord[T]) Sorted() []T {
	c := slices.Clone(o.vals)
	slices.Sort(c)
	return c
}
func (o *Ord[T]) Adder() func(T) { return func(v T) { o.Add(v) } }

type myErr struct{}

func (myErr) Error() string { return "e" }

type K struct {
	A int
	B any
}

func main() {
	println(Kind(1), Kind(int8(1)), Kind(uint8(1)), Kind("s"), Kind([]int{}), Kind([]string{}))
	println(Kind[any](nil), Kind[error](nil), Kind[error](myErr{}), Kind(A4{}), Kind(3.5))
	println(KindPtr(5), KindPtr("x"), Kind[*int](nil))
	println(Kind[[]any]([]any{}), Kind[any]([]any{}))

	println(Eq(1, 1), Eq("a", "b"), Eq[any](1, 1), Eq[any](1, int64(1)))
	println(Eq[any]([]int{1}, []int{1}))
	println(Eq[any]([]int{1}, "x"))
	println(Eq(K{1, 2}, K{1, 2}), Eq(K{1, map[int]int{}}, K{1, map[int]int{}}), Eq(K{1, map[int]int{}}, K{2, map[int]int{}}))
	println(Eq[[2]any]([2]any{1, func() {}}, [2]any{1, func() {}}))
	println(Eq[error](myErr{}, myErr{}), Eq[error](nil, myErr{}))
	println(Index([]any{1, "a", 2.5, K{1, "z"}}, any(K{1, "z"})), Index([]string{"a", "b"}, "b"), Index([]float64{0, -1}, -1))

	println(Lens(A4{1, 2, 3, 4}))
	println(Lens(A8{}))
	println(ArrLen([3]string{}), ArrLen([5]string{"a"}))
	a4 := A4{1, 2, 3, 4}
	println(SumFirst(a4), a4[0], SumFirst([8]int{1, 1, 1, 1, 1, 1, 1, 1}))

	calls := 0
	sq, hits := Memo(func(n int) int { calls++; return n * n })
	println(sq(3), sq(4), sq(3), sq(3), calls, hits())
	up, h2 := Memo(func(p [2]string) string { return p[1] + p[0] })
	println(up([2]string{"a", "b"}), up([2]string{"a", "b"}), up([2]string{"b", "a"}), h2())

	toS := Compose(func(i int) float64 { return float64(i) / 2 }, func(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) })
	toL := Compose(toS, func(s string) int { return len(s) })
	println(toS(7), toL(12345))

	var oi Ord[int]
	oi.Add(3).Add(1).Add(2)
	add := oi.Adder()
	add(0)
	s := oi.Sorted()
	println(len(s), s[0], s[3], oi.vals[0])
	var os Ord[string]
	adds := os.Adder()
	for _, w := range []string{"pear", "apple", "fig"} {
		adds(w)
	}
	ss := os.Sorted()
	println(ss[0], ss[1], ss[2])
	var of Ord[float64]
	of.Add(2).Add(-0.5).Add(1e-9)
	fs := of.Sorted()
	println(fs[0] < 0, fs[1] < 1e-8, fs[2] == 2, cmp.Compare(fs[0], fs[1]), cmp.Compare("b", "a"))
	println(max(3, 7, 1), min(2.5, -1.0), max("a", "b", "ab"), min(int8(-128), int8(127)))
}
