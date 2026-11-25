package main

func main() {
	a := 0xF0 & 0x3C
	b := 0xF0 | 0x0F
	c := 0xFF ^ 0x0F
	d := 0xFF &^ 0x0F
	e := ^a
	f := a + b + c + d + e
	f = f
}

// observe: a b c d e f
