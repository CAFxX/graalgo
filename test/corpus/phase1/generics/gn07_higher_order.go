package main

// Apply is higher-order: the type param T flows through a func type.
func Apply[T any](f func(T) T, x T) T {
	return f(x)
}

// Compose chains two instantiations of a generic function value.
func Compose[A, B, C any](f func(B) C, g func(A) B, x A) C {
	return f(g(x))
}

func inc(x int) int { return x + 1 }

func dbl(x int) int { return x * 2 }

func main() {
	a := Apply[int](inc, 5)
	b := Apply[int](dbl, 5)
	c := Apply(inc, 10)
	d := Compose[int, int, int](dbl, inc, 5)
	e := 0
	if Apply[string](func(s string) string { return s + "!" }, "hi") == "hi!" {
		e = 1
	}
	a = a
	b = b
	c = c
	d = d
	e = e
}

// verifies: GG-0014
// observe: a b c d e
