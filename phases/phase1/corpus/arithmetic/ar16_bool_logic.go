package main

func main() {
	var t bool = true
	var f bool = false
	a := t && f
	b := t || f
	c := !t
	d := !f
	e := (t && t) || f
	g := f || (t && !f)
	h := !(t && f)
	a = a
	b = b
	c = c
	d = d
	e = e
	g = g
	h = h
}

// observe: a b c d e g h
