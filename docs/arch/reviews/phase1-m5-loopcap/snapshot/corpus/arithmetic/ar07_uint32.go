package main

func main() {
	var a uint32 = 4000000000
	var b uint32 = 3000000000
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	h := b - a
	i := a >> 31
	j := a >> 32
	k := a << 32
	m := ^a
	var cnt uint = 1
	n := a << cnt
	o := a & b
	p := a | b
	q := a ^ b
	w := c + d + e + f + g + h + i + j + k + m + n + o + p + q
	w = w
}

// observe: c d e f g h i j k m n o p q w
