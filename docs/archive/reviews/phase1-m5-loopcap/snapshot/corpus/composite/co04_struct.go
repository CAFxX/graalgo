package main

func main() {
	type point struct {
		x int
		y int
	}

	// unkeyed struct literal
	p := point{3, 4}
	a := p.x + p.y
	// keyed struct literal
	q := point{y: 10, x: 1}
	b := q.x + q.y
	// field assignment
	p.x = 30
	c := p.x + p.y
	// struct value semantics: assignment copies
	r := q
	r.y = 99
	d := q.y + r.y
	// zero value struct
	var z point
	e := z.x + z.y
	// pointer to struct literal
	s := &point{5, 6}
	f := s.x + s.y
	_ = a
	_ = b
	_ = c
	_ = d
	_ = e
	_ = f
}

// observe: a b c d e f
