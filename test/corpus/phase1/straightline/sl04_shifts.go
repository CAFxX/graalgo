package main

func main() {
	var s uint64 = 1
	a := s << 64
	b := s << 65
	var cnt uint = 3
	c := s << cnt
	var big uint64 = 0x8000000000000000
	d := big >> 63
	e := big >> 64
	u := a + b + c + d + e
	var n int64 = -8
	f := n >> 64
	g := n >> 65
	h := n << 65
	var p int64 = 8
	k := p >> 64
	m := p << 64
	j := f + g + h + k + m
	// P0 fix-p0-shift-unsigned-count: an unsigned count is never
	// negative — a high-bit-set pattern is a huge positive count
	// (Go sets no upper limit on the shift count), never a panic.
	var huge uint64 = 1<<64 - 1
	var i8 int8 = 1
	// (int64() only widens the observed slot: the shift itself still
	// runs at int8/int32 width — the straightline harness reads Long
	// slots only.)
	q1 := int64(i8 << huge) // 0, no panic
	var u64 uint64 = 0x8000000000000000
	q2 := u64 >> huge // 0
	var nn int64 = -8
	q3 := nn >> huge // -1 (sign extension)
	var pp int64 = 8
	q4 := pp >> huge // 0
	var i32 int32 = -1
	q5 := int64(i32 << huge) // 0
	var c8 uint8 = 255       // sub-64 unsigned count, zero-extended to 255
	q6 := s << c8            // 0
	var c3 uint8 = 3         // small unsigned count shifts normally
	q7 := s << c3            // 8
	u = u
	j = j
	q1 = q1
	q2 = q2
	q3 = q3
	q4 = q4
	q5 = q5
	q6 = q6
	q7 = q7
}

// observe: a b c d e u f g h k m j q1 q2 q3 q4 q5 q6 q7
