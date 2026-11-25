package main

func main() {
	var w uint64 = 0xFFFFFFFFFFFFFFFF
	a := w / 2
	b := w % 3
	c := w >> 1
	d := w >> 63
	e := w >> 64
	f := w << 64
	g := w + 1
	h := 0 - w
	i := w * w
	j := w & 0x0F0F0F0F0F0F0F0F
	k := w | 0x1234
	m := w ^ 0x1234
	n := w &^ 0xFF
	o := ^w
	var cnt uint = 63
	p := w >> cnt
	q := a + b + c + d + e + f + g + h + i + j + k + m + n + o + p
	q = q
}

// observe: a b c d e f g h i j k m n o p q
