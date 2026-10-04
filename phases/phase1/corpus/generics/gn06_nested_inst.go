package main

func Wrap[T any](x T) []T {
	return []T{x}
}

// DoubleWrap instantiates Wrap[T] from inside a generic body: the fixpoint
// must discover Wrap[int] via DoubleWrap[int]'s instantiation.
func DoubleWrap[T any](x T) [][]T {
	return [][]T{Wrap[T](x)}
}

// Mutually recursive instantiation: Even calls Odd and vice versa, each
// at a different type argument. gc rejects infinite instantiation, so the
// fixpoint terminates.
func Even[T any](x T, n int) T {
	if n <= 0 {
		return x
	}
	return Odd[T](x, n-1)
}

func Odd[T any](x T, n int) T {
	if n <= 0 {
		return x
	}
	return Even[T](x, n-1)
}

func main() {
	a := DoubleWrap[int](5)[0][0]
	b := Even[int](42, 10)
	c := Odd[int](7, 9)
	d := DoubleWrap[string]("hi")[0][0]
	e := 0
	if d == "hi" {
		e = 1
	}
	a = a
	b = b
	c = c
	d = d
	e = e
}

// verifies: GG-0014
// observe: a b c e
