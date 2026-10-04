package main

func main() {
	a, b := 1, 2
	a, b = b, a
	c, d, e := 10, 20, 30
	c, d, e = e+1, c+1, d+1
	f := 5
	f += 3
	f *= 2
	f <<= 1
	g := 81
	g >>= 2
	g &= 3
	h := a + b + c + d + e + f + g
	h = h
}

// observe: a b c d e f g h
