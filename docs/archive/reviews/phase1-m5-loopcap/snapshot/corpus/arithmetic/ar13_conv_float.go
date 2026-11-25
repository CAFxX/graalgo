package main

func main() {
	var u uint64 = 0xFFFFFFFFFFFFFFFF
	a := float64(u)
	b := float32(u)
	var i int64 = -3
	c := float64(i)
	d := float32(int32(16777217))
	var x float64 = 123.999
	e := int64(x)
	f := int64(-x)
	big := 1e300
	g := int64(big)
	h := uint64(big)
	j := int32(big)
	k := uint32(big)
	m := int16(big)
	n := uint8(big)
	h2 := 9.3e18
	o := uint64(h2)
	p := float64(uint64(9007199254740993))
	f32 := float32(3.99)
	q := int64(f32)
	r := float64(float32(0.1))
	s := float32(0.1 + 0.2)
	w := float64(-(1 << 70))
	a = a
	b = b
	c = c
	d = d
	e = e
	f = f
	g = g
	h = h
	j = j
	k = k
	m = m
	n = n
	o = o
	p = p
	q = q
	r = r
	s = s
	w = w
}

// observe: a b c d e f g h j k m n o p q r s w
