package main

func main() {
	var f1 float64 = 5e9
	var f2 float64 = 3e9
	var f3 float64 = 300.7
	var f4 float64 = 70000.9
	var f5 float64 = -1.5
	var f6 float32 = 300.7
	c1 := uint32(f1) == uint32(705032704)
	c2 := int32(f2) == int32(-1294967296)
	c3 := uint32(f1) == uint32(f1)
	c4 := int8(f3) == 44
	c5 := int16(f4) == 4464
	c6 := uint8(f5) == 255
	c7 := int32(f2) == -2147483648
	c8 := int8(f6) == 44
	u1 := uint32(f1)
	i1 := int32(f2)
	s := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49}
	v := s[int8(f3)]
	sub := s[uint8(f3):]
	ln := len(sub)
	c1 = c1
	c2 = c2
	c3 = c3
	c4 = c4
	c5 = c5
	c6 = c6
	c7 = c7
	c8 = c8
	u1 = u1
	i1 = i1
	v = v
	ln = ln
}

// observe: c1 c2 c3 c4 c5 c6 c7 c8 u1 i1 v ln
