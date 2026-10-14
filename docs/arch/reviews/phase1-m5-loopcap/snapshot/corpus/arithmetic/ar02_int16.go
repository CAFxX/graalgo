package main

func main() {
	var a int16 = 30000
	var b int16 = 2000
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	var h int16 = -32768
	i := h / -1
	j := h >> 15
	k := h << 1
	m := a & b
	n := a | b
	o := a ^ b
	p := ^h
	q := -h
	r := a << 16
	s := a >> 16
	w := c + d + e + f + g + i + j + k + m + n + o + p + q + r + s
	w = w
}

// observe: c d e f g i j k m n o p q r s w
