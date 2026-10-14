package main

func main() {
	var a uint16 = 60000
	var b uint16 = 10000
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	h := b - a
	i := ^a
	j := a << 4
	k := a >> 4
	m := a >> 16
	n := a << 16
	o := a & b
	p := a | b
	w := c + d + e + f + g + h + i + j + k + m + n + o + p
	w = w
}

// observe: c d e f g h i j k m n o p w
