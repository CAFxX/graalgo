// Constant folding: iota patterns (skips, expressions, shifts, typed iota),
// huge untyped integer/float constants folded down to typed values, exact
// rational arithmetic of untyped constants, rune/byte constants, typed
// constant expressions, and comparison of folded vs runtime-computed values.
package main

import "math"

type Weekday uint8

const (
	Sun Weekday = iota
	Mon
	Tue
	_
	Thu
	Fri = iota * 10
	Sat
)

const (
	_  = iota
	KB = 1 << (10 * iota)
	MB
	GB
	TB
	PB
	EB
)

const (
	a0, b0 = iota, -iota
	a1, b1
	a2, b2 = iota * iota, iota << iota
	a3, b3
)

const (
	m0 int8 = -1 << iota
	m1
	m2
	m3
	m4
	m5
	m6
	m7
)

const huge = 1 << 200
const hugeDiv = huge >> 190
const third = 1.0 / 3
const exact = third * 3
const bigf = 1e400
const smallf = 1e-400
const ratio = bigf / 1e399

const r1 = 'a' + 1
const r2 = '世' - 'a'
const b1v byte = 'z' - 'a'
const mixed = 'x' * 2.5

var rt3 = 3.0

func id(x float64) float64 { return x }

func main() {
	println("weekday", Sun, Mon, Tue, Thu, Fri, Sat)
	println("sizes", KB, MB, GB, TB, PB, EB, EB>>60)
	println("pairs", a0, b0, a1, b1, a2, b2, a3, b3)
	println("m", m0, m1, m2, m3, m4, m5, m6, m7)
	println("huge", hugeDiv, huge>>138, uint64(huge>>137), huge/(huge>>10), huge%7)
	println("third", third, exact, exact == 1, third*3 == 1, id(1)/rt3*rt3 == 1, id(1.0)/3)
	println("bigf", ratio, smallf*1e400, float32(bigf/1e362), bigf*smallf)
	println("runes", r1, r2, b1v, mixed, string(rune(r1)), 'a'*'b', '\x7f'+'ÿ', '\U0010FFFF')
	const c1 = math.MaxInt64 + 1 - 1
	const c2 = math.MinInt64 / -1 / -1
	const c3 = -math.MaxUint64 + math.MaxUint64
	println("bounds", c1, c2, c3, math.MaxUint32, math.MinInt32, uint64(math.MaxUint64))
	const f1 = math.MaxFloat64 * 1e10 / 1e10
	const f2 = math.SmallestNonzeroFloat64 / 2 * 2
	println("fbounds", f1, f2, float32(math.MaxFloat32), math.SmallestNonzeroFloat32)
	const pi = math.Pi
	println("pi", pi, float32(pi), pi*1e100/1e100, int(pi*1e15/pi), int64(pi*(1<<62)/pi))
	// typed constant arithmetic: wraps are compile errors, so use exact ones
	const t8 int8 = 127
	const tu uint16 = 65535
	println("typed", t8/2, t8%10, tu>>4, tu&^0xff, ^tu, -t8-1)
	// untyped constant vs runtime for division
	const n = -7
	var vn = -7
	println("div", n/2, vn/2, n%3, vn%3, n>>1, vn>>1, -n/2, n/2.0)
	// constants in float precision: 0.1+0.2 folded exactly
	const s = 0.1 + 0.2
	var v1, v2 = 0.1, 0.2
	println("pt3", s, s == 0.3, v1+v2, v1+v2 == 0.3, float32(s), float32(v1)+float32(v2))
	// conversions of untyped float constants to int (must be exact)
	const ff = 1e18
	println("ffint", int64(ff), uint64(ff*18), int32(1e9), int8(-128.0), uint8(255.0))
	// complex constants
	const cc = (1 + 2i) * (3 - 4i) / (5i)
	println("cc", cc, real(cc), imag(cc), complex64(cc))
	// iota in expression arrays
	arr := [...]int{iota0(), 1 << 3, 077, 0o17, 0x1F, 0b1011, 1_000_000, 0x_FF, 0b_1_0}
	for i, x := range arr {
		println("lit", i, x)
	}
	fl := []float64{0x1p-2, 0x1.8p1, 0x.8p0, 1_0.5e1, 0x1p+1023, 0x1p-1074, 1e-7, .5, 5.}
	for i, x := range fl {
		println("flit", i, x)
	}
	println("weekday arith", Sat-Fri, Fri/Mon, Weekday(255)-Sat+Fri, ^Sun)
}

func iota0() int { return 0 }
