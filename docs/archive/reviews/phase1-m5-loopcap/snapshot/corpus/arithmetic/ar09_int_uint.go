package main

func main() {
	var i int = -5
	a := i + 3
	b := i - 3
	c := i * -4
	d := i / 3
	e := i % 3
	var u uint = 7
	f := u + 3
	g := u - 10
	h := u * u
	k := u / 3
	m := u % 3
	var p uintptr = 0xFFFFFFFFFFFFFFFF
	n := p >> 63
	o := p / 2
	qi := a + b + c + d + e
	qu := f + h + k + m
	qp := n + o
	r := g
	qi = qi
	qu = qu
	qp = qp
	r = r
}

// observe: a b c d e f g h k m n o qi qu qp r
