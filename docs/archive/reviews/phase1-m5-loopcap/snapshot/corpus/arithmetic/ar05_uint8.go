package main

func main() {
	var a uint8 = 200
	var b uint8 = 100
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	h := b - a
	i := a & b
	j := a | b
	k := a ^ b
	m := a &^ b
	n := ^a
	o := a << 2
	p := a >> 2
	q := a >> 8
	r := a << 8
	w := c + d + e + f + g + h + i + j + k + m + n + o + p + q + r
	w = w
}

// observe: c d e f g h i j k m n o p q r w
