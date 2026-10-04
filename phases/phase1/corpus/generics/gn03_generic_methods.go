package main

type Stack[T any] struct {
	elems []T
	n     int
}

func (s Stack[T]) Size() int {
	return s.n
}

func (s Stack[T]) Get(i int) T {
	return s.elems[i]
}

func main() {
	si := Stack[int]{elems: []int{10, 20, 30}, n: 3}
	a := si.Size()
	b := si.Get(1)
	c := si.Get(0) + si.Get(2)
	ss := Stack[string]{elems: []string{"x", "y"}, n: 2}
	d := ss.Size()
	e := 0
	if ss.Get(0) == "x" && ss.Get(1) == "y" {
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
