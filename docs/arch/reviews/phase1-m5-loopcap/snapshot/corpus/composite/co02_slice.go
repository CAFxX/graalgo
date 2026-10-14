package main

func main() {
	s := []int{1, 2, 3, 4, 5}
	// 2-index slicing shares the backing array (aliasing)
	t := s[1:4]
	t[0] = 99
	a := s[1] + t[0] + t[2]
	// slice of slice
	u := s[0:2]
	v := u[1]
	// open-ended slices
	w := s[:3]
	x := s[2:]
	y := w[2] + x[0]
	// index assignment through slice
	s[4] = 50
	z := s[4] + t[2]
	_ = a
	_ = v
	_ = y
	_ = z
}

// observe: a v y z
