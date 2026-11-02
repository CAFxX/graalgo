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
	// Negative float -> uint64: gc converts via int64 then reinterprets
	// the bits, so these wrap (or saturate below -2^63) instead of
	// yielding 0x8000000000000000 for every negative value.
	neg1 := -1.5
	uneg1 := uint64(neg1)
	neg2 := -2.5
	uneg2 := uint64(neg2)
	neg3 := -2147483649.0
	uneg3 := uint64(neg3)
	neg32 := float32(-1.5)
	uneg32 := uint64(neg32)
	negMin := -9223372036854775808.0
	unegMin := uint64(negMin)
	negBig := -9.3e18
	unegBig := uint64(negBig)
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
	neg1 = neg1
	uneg1 = uneg1
	neg2 = neg2
	uneg2 = uneg2
	neg3 = neg3
	uneg3 = uneg3
	neg32 = neg32
	uneg32 = uneg32
	negMin = negMin
	unegMin = unegMin
	negBig = negBig
	unegBig = unegBig
}

// observe: a b c d e f g h j k m n o p q r s w neg1 uneg1 neg2 uneg2 neg3 uneg3 neg32 uneg32 negMin unegMin negBig unegBig
