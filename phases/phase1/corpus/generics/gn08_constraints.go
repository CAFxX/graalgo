package main

// Constraint edges: comparable, tilde (underlying), and interface bounds.

func Find[T comparable](s []T, v T) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

type MyInt int

// Tilde constraint: T's underlying type is int.
func Double[T ~int](x T) T {
	return x + x
}

type Adder interface {
	Add(int) int
}

type Acc struct{ total int }

func (a Acc) Add(x int) int { return a.total + x }

// Interface bound with a method call on the bound value.
func UseAdder[T Adder](t T, x int) int {
	return t.Add(x)
}

func main() {
	a := Find[int]([]int{5, 6, 7}, 6)
	b := Find[string]([]string{"a", "b"}, "z")
	c := int(Double[MyInt](21))
	d := UseAdder[Acc](Acc{total: 100}, 23)
	a = a
	b = b
	c = c
	d = d
}

// verifies: GG-0014
// observe: a b c d
