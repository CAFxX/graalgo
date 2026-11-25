package main

func main() {
	var a float32 = 0.1
	var b float32 = 0.2
	c := a + b
	d := a * b
	e := a / b
	f := b - a
	g := -a
	h := a + a + a
	i := float32(16777217)
	j := float32(16777219)
	big := float32(1e30)
	k := big * big
	m := k + 1
	n := 1 / k
	o := -k
	p := float32(0.5) / 2
	c = c
	d = d
	e = e
	f = f
	g = g
	h = h
	i = i
	j = j
	k = k
	m = m
	n = n
	o = o
	p = p
}

// observe: c d e f g h i j k m n o p
