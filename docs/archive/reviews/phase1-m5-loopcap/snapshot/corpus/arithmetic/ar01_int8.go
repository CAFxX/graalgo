package main

func main() {
	var a int8 = 100
	var b int8 = 30
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	var h int8 = -100
	i := h / 3
	j := h % 3
	k := h / -1
	m := a & b
	n := a | b
	o := a ^ b
	p := a &^ b
	q := ^a
	r := -a
	s := a << 2
	t := a >> 2
	var z int8 = -128
	u := z >> 7
	v := z / -1
	w := c + d + e + f + g + i + j + k + m + n + o + p + q + r + s + t + u + v
	w = w
}

// observe: c d e f g i j k m n o p q r s t u v w
