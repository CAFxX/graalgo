package main

func main() {
	s := "hello"
	// string indexing gives bytes
	a := int(s[0]) + int(s[4])
	// string slicing
	t := s[1:4]
	b := int(t[0]) + int(t[2])
	// slice to end / from start
	u := s[:2]
	v := s[3:]
	c := int(u[1]) + int(v[1])
	// string range already covered in cf07; here compare bytes via loop
	sum := 0
	for i := 0; i < 5; i++ {
		sum += int(s[i])
	}
	_ = a
	_ = b
	_ = c
	_ = sum
}

// observe: a b c sum
