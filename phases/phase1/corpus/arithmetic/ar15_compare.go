package main

func main() {
	var i int64 = -5
	var j int64 = 3
	c1 := i < j
	c2 := i == j
	c3 := i >= j
	var u uint64 = 0xFFFFFFFFFFFFFFFF
	c4 := u > 3
	c5 := u < 3
	var f float64 = 0.5
	c6 := f < 1.0
	c7 := f == 0.5
	c8 := f != 0.5
	big := 1e308
	inf := big * 10
	c9 := inf > big
	c10 := inf == inf
	c11 := inf < big
	var s1 string = "abc"
	var s2 string = "abd"
	c12 := s1 < s2
	c13 := s1 == s2
	var b1 bool = true
	var b2 bool = false
	c14 := b1 == b2
	c15 := b1 != b2
	var i32 int32 = -1
	var u32 uint32 = 1
	c16 := i32 < 0
	c17 := u32 > 0
	c1 = c1
	c2 = c2
	c3 = c3
	c4 = c4
	c5 = c5
	c6 = c6
	c7 = c7
	c8 = c8
	c9 = c9
	c10 = c10
	c11 = c11
	c12 = c12
	c13 = c13
	c14 = c14
	c15 = c15
	c16 = c16
	c17 = c17
}

// observe: c1 c2 c3 c4 c5 c6 c7 c8 c9 c10 c11 c12 c13 c14 c15 c16 c17
