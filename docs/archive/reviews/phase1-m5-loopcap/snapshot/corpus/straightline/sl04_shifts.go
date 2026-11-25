package main

func main() {
	var s uint64 = 1
	a := s << 64
	b := s << 65
	var cnt uint = 3
	c := s << cnt
	var big uint64 = 0x8000000000000000
	d := big >> 63
	e := big >> 64
	u := a + b + c + d + e
	var n int64 = -8
	f := n >> 64
	g := n >> 65
	h := n << 65
	var p int64 = 8
	k := p >> 64
	m := p << 64
	j := f + g + h + k + m
	u = u
	j = j
}

// observe: a b c d e u f g h k m j
