package main

func add(a, b int) int {
	return a + b
}

func mul(a, b int) int {
	return a * b
}

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

func applyTwice(f func(int) int, x int) int {
	return f(f(x))
}

func main() {
	a := add(3, 4)
	b := mul(5, 6)
	c := fact(5)
	d := add(mul(2, 3), fact(3))
	e := applyTwice(func(x int) int { return x + 1 }, 10)
	a = a
	b = b
	c = c
	d = d
	e = e
}

// verifies: GG-0014
// observe: a b c d e
