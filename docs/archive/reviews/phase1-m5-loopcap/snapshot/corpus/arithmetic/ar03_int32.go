package main

func main() {
	var a int32 = 2000000000
	var b int32 = 1500000000
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	var h int32 = -2147483648
	i := h / -1
	j := h % -1
	k := h >> 31
	m := h >> 32
	n := h << 32
	o := a << 2
	p := ^a
	q := -a
	var cnt uint = 33
	r := a >> cnt
	w := c + d + e + f + g + i + j + k + m + n + o + p + q + r
	w = w
}

// observe: c d e f g i j k m n o p q r w
