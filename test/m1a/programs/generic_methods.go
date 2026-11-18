package main

// Generic methods: a method with its own type parameters on a generic type,
// reached through calls (explicit and inferred method type arguments), method
// values and method expressions, at several instantiations of both the
// receiver and the method.

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

func (s Stack[T]) Fold[U any](init U, f func(U, T) U) U {
	acc := init
	for _, x := range s.items {
		acc = f(acc, x)
	}
	return acc
}

func (s Stack[T]) MapTo[U, V any](f func(T) U, g func(U) V) []V {
	var out []V
	for _, x := range s.items {
		out = append(out, g(f(x)))
	}
	return out
}

type Plain struct{ n int }

func (p Plain) Scale[N ~int | ~float64](k N) N { return N(p.n) * k }

func main() {
	var is Stack[int]
	is.Push(4)
	is.Push(5)
	var ss Stack[string]
	ss.Push("a")
	ss.Push("bc")

	// Calls: explicit and inferred method type arguments.
	println(is.Fold[int](1, func(a, b int) int { return a * b }))
	println(is.Fold[string]("", func(a string, b int) string { return a + "#" }))
	println(is.Fold(10, func(a, b int) int { return a + b }))
	println(is.Fold("x", func(a string, b int) string { return a + "." }))
	println(ss.Fold(0, func(a int, b string) int { return a + len(b) }))
	println(len(is.MapTo[string, bool](func(i int) string { return "v" }, func(s string) bool { return s == "v" })))
	println(len(ss.MapTo(func(s string) int { return len(s) }, func(n int) float64 { return float64(n) / 2 })))

	// Method values.
	f1 := is.Fold[string]
	println(f1("<", func(a string, b int) string { return a + "!" }))
	f2 := ss.Fold[int]
	println(f2(100, func(a int, b string) int { return a - len(b) }))
	f3 := is.MapTo[int, int]
	println(f3(func(i int) int { return i * i }, func(i int) int { return i + 1 })[1])

	// Method expressions.
	e1 := Stack[int].Fold[string]
	println(e1(is, "", func(a string, b int) string { return a + "#" }))
	e2 := Stack[string].Fold[float64]
	println(e2(ss, 0.5, func(a float64, b string) float64 { return a + float64(len(b)) }))
	println(Stack[int].Fold[int](is, 0, func(a, b int) int { return a + b }))
	e3 := (*Stack[int]).Fold[int]
	println(e3(&is, 7, func(a, b int) int { return a + b }))

	// A generic method on a non-generic type.
	p := Plain{3}
	println(p.Scale[int](4), p.Scale(1.5), Plain.Scale[float64](p, 2.5))
	sc := p.Scale[int]
	println(sc(5))
}
