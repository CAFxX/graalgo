package main

func main() {
	a := 42
	b := -a
	c := ^a
	d := -(-a)
	e := ^(-b)
	f := -(c)
	g := a + b + c + d + e + f
	g = g
}

// observe: a b c d e f g
