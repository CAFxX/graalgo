// math/bits multi-word arithmetic: Add/Sub/Mul/Div/Rem at 32, 64 and uint
// widths with carry/borrow in and out, Div panics (divide by zero and
// quotient overflow, recovered and printed), and a 128-bit multiply /
// divide round trip built from them.
package main

import "math/bits"

func catch(name string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			println(name, "no panic")
			return
		}
		if e, ok := r.(interface{ Error() string }); ok {
			println(name, "panic:", e.Error())
		} else {
			println(name, "panic other")
		}
	}()
	f()
}

var v64 = []uint64{0, 1, 2, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff, 0xfffffffffffffffe,
	0x00000000ffffffff, 0x0000000100000000, 0x123456789abcdef0, 0xdeadbeefcafebabe}
var v32 = []uint32{0, 1, 2, 0x7fffffff, 0x80000000, 0xffffffff, 0xfffffffe, 0xffff, 0x10000, 0x12345678}

func main() {
	for _, a := range v64 {
		for _, b := range v64 {
			for c := uint64(0); c <= 1; c++ {
				s, co := bits.Add64(a, b, c)
				d, bo := bits.Sub64(a, b, c)
				println("as64", a, b, c, s, co, d, bo)
			}
			hi, lo := bits.Mul64(a, b)
			println("mul64", a, b, hi, lo)
			if b > hi {
				q, r := bits.Div64(hi, lo, b)
				println("div64", hi, lo, b, q, r, bits.Rem64(hi, lo, b))
			}
			if b != 0 {
				println("rem64", a, b, bits.Rem64(a, b, b), bits.Rem64(b, a, b))
			}
		}
	}
	for _, a := range v32 {
		for _, b := range v32 {
			s, co := bits.Add32(a, b, 1)
			d, bo := bits.Sub32(a, b, 1)
			hi, lo := bits.Mul32(a, b)
			println("32", a, b, s, co, d, bo, hi, lo)
			if b > hi {
				q, r := bits.Div32(hi, lo, b)
				println("div32", q, r, bits.Rem32(hi, lo, b))
			}
			u, uc := bits.Add(uint(a), uint(b)<<32, 0)
			m, ml := bits.Mul(uint(a)<<32, uint(b)<<16)
			println("uint", u, uc, m, ml)
		}
	}
	var zero uint64
	var z32 uint32
	catch("div64 zero", func() { bits.Div64(0, 1, zero) })
	catch("div64 ovf", func() { bits.Div64(5, 1, v64[4]>>61) })
	catch("div64 eq", func() { bits.Div64(7, 0, 7) })
	catch("div32 zero", func() { bits.Div32(0, 1, z32) })
	catch("div32 ovf", func() { bits.Div32(0xffffffff, 0, 0xfffffffe) })
	catch("div uint", func() { bits.Div(1, 0, uint(zero)) })
	catch("rem64 zero", func() { bits.Rem64(1, 2, zero) })
	catch("rem64 big ok", func() { println(bits.Rem64(0xffffffffffffffff, 0xffffffffffffffff, 3)) })
	catch("rem32 zero", func() { bits.Rem32(1, 2, z32) })

	// 128-bit factorial via Mul64/Add64, then divide back
	hi, lo := uint64(0), uint64(1)
	for k := uint64(1); k <= 34; k++ {
		h1, l1 := bits.Mul64(lo, k)
		_, h2 := bits.Mul64(hi, k)
		hi = h1 + h2
		lo = l1
		println("fact", k, hi, lo)
	}
	for k := uint64(34); k >= 1; k-- {
		qh := hi / k
		rh := hi % k
		ql, r := bits.Div64(rh, lo, k)
		hi, lo = qh, ql
		if r != 0 {
			println("nonzero rem", k, r)
		}
	}
	println("back", hi, lo)
	// carry chain across a 4-word number
	var x [4]uint64 = [4]uint64{0xffffffffffffffff, 0xffffffffffffffff, 0xfffffffffffffffe, 0}
	var c uint64 = 1
	for i := range x {
		x[i], c = bits.Add64(x[i], 0, c)
	}
	println("chain", x[0], x[1], x[2], x[3], c)
	var bw uint64 = 1
	for i := range x {
		x[i], bw = bits.Sub64(x[i], 0, bw)
	}
	println("chainb", x[0], x[1], x[2], x[3], bw)
}
