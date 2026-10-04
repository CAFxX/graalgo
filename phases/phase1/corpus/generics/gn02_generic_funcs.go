package main

func Min[T int | string](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func Max[T int](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func main() {
	a := Min[int](3, 4)
	b := Min[int](10, 20)
	c := Min(10, 20)
	d := 0
	if Min("b", "a") == "a" {
		d = 1
	}
	e := 0
	if Min[string]("x", "y") == "x" {
		e = 1
	}
	f := Max[int](7, 9)
	g := Max(1, 2)
	a = a
	b = b
	c = c
	d = d
	e = e
	f = f
	g = g
}

// verifies: GG-0014
// observe: a b c d e f g
