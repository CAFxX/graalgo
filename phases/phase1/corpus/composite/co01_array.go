package main

func main() {
	// array literal, indexing, index assignment, index incdec
	var a [4]int
	a[0] = 10
	a[1] = 20
	a[3] = 40
	s := a[0] + a[1] + a[2] + a[3]
	a[2]++
	t := a[2]
	// keyed array literal
	b := [3]int{2: 7, 0: 1}
	u := b[0] + b[1] + b[2]
	// array value semantics: assignment copies
	c := b
	c[0] = 99
	v := b[0] + c[0]
	_ = s
	_ = t
	_ = u
	_ = v
}

// observe: s t u v
