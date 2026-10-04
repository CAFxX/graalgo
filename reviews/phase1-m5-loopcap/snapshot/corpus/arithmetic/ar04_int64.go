package main

func main() {
	var a int64 = 9223372036854775807
	var b int64 = 2
	c := a + 1
	d := a * b
	e := a / b
	f := a % b
	g := -a
	h := a >> 62
	i := a >> 63
	j := a >> 64
	k := b << 62
	m := b << 63
	n := a & b
	o := a | b
	p := a ^ b
	q := a &^ b
	var v int64 = -8
	r := v >> 64
	s := v >> 65
	t := v << 65
	w := c + d + e + f + g + h + i + j + k + m + n + o + p + q + r + s + t
	w = w
}

// observe: c d e f g h i j k m n o p q r s t w
