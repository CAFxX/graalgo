package main

func main() {
	var r int = 65
	a := string(r)
	b := string(0x1F600)
	c := string(-1)
	d := string(0xD800)
	e := string(0x110000)
	f := string(0)
	g := string(int8(90))
	h := a + "bc"
	i := h + b
	var s1 string = "abc"
	var s2 string = "abd"
	k := s1 < s2
	m := s1 == s2
	n := s2 > s1
	o := s1 + s2
	p := "abc" < "abd"
	q := string(-(1 << 70))
	a = a
	b = b
	c = c
	d = d
	e = e
	f = f
	g = g
	h = h
	i = i
	k = k
	m = m
	n = n
	o = o
	p = p
	q = q
}

// observe: a b c d e f g h i k m n o p q
