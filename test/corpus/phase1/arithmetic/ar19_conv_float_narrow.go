package main

// B-P1-1 (REVIEW-2026-11-09-13): float->narrow-int conversions consumed
// through executeLong — comparisons, arithmetic, and widening to int/int64.
// The int32-typed locals in ar13 are read via executeInt (correct), which
// is why this bug survived: only executeLong consumers observe it.
// NaN comes from a runtime 0.0/0.0 division (a variable, not a constant).
func main() {
	var zero float64
	nan := zero / zero
	f3e9 := 3e9
	fm3e9 := -3e9
	f200 := 200.0
	fm200 := -200.0
	f70k := 70000.0
	f300 := 300.0
	fm1 := -1.0
	f5e9 := 5e9
	big32 := 4294967340.0
	big32b := 4294967298.0
	big40 := 1099511627812.0
	// Comparisons through executeLong (BinaryOpNode reads operands via executeLong).
	a := int32(nan) == -2147483648
	b := int32(f3e9) < 0
	c := int32(nan) == int32(f3e9)
	d := int32(fm3e9) == -2147483648
	// Arithmetic through executeLong.
	e := int32(f3e9) + 1
	f := int32(fm3e9) + 1
	// Widening: the narrowed value, observed as int/int64.
	g := int(int8(f200))
	h := int(int8(fm200))
	i := int(int16(f70k))
	j := int(uint8(f300))
	k := int(uint16(f70k))
	m := int64(uint32(nan))
	n := int64(uint32(f5e9))
	o := int64(uint32(fm1))
	// Saturate-not-truncate discriminators (wrong under int64-then-truncate).
	p := int(int8(big32))
	q := int(int16(big32b))
	r := int(uint8(big40))
	// float32 source.
	var zero32 float32
	nan32 := zero32 / zero32
	f32_3e9 := float32(f3e9)
	f32_300 := float32(f300)
	s := int32(nan32) == -2147483648
	t := int32(f32_3e9) == -2147483648
	u := int(uint8(f32_300))
	v := int64(uint32(nan32))
	a = a
	b = b
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
	q = q
	r = r
	s = s
	t = t
	u = u
	v = v
}

// observe: a b c d e f g h i j k m n o p q r s t u v
