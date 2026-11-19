// Shifts of every width by variable counts: counts >= width (0 or sign
// fill), signed and unsigned count types, arithmetic vs logical right shift,
// negative signed shift count panics (recovered), constant shifts, rotate
// idioms, and shift results converted between widths.
package main

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

var counts = []uint{0, 1, 2, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 255, 256, 1 << 20, 1<<64 - 1}
var scounts = []int{0, 1, 3, 7, 8, 31, 32, 63, 64, 100, 1 << 40}

var vi8 int8 = -101
var vu8 uint8 = 0xb5
var vi16 int16 = -12345
var vu16 uint16 = 0xbeef
var vi32 int32 = -1234567891
var vu32 uint32 = 0xdeadbeef
var vi64 int64 = -0x123456789abcdef0
var vu64 uint64 = 0xfedcba9876543210
var vi int = -77
var vup uintptr = 0x8000000000000001

func main() {
	for _, s := range counts {
		println("u", s, vi8<<s, vi8>>s, vu8<<s, vu8>>s, vi16<<s, vi16>>s, vu16<<s, vu16>>s)
		println("w", s, vi32<<s, vi32>>s, vu32<<s, vu32>>s, vi64<<s, vi64>>s, vu64<<s, vu64>>s, vi<<s, vi>>s, vup<<s, vup>>s)
	}
	for _, s := range scounts {
		println("s", s, vi8<<s, vi8>>s, vu16>>s, vi32<<s, vi32>>s, vu64>>s, vi64>>s, vi64<<s)
	}
	// narrow count types
	for c := uint8(0); c < 70; c += 3 {
		println("c8", c, vu64>>c, vi64>>c, vu32<<c, int8(1)<<c, uint16(1)<<c)
	}
	for c := int8(0); c < 70; c += 5 {
		println("ci8", c, vu32>>c, vi16<<c, vi16>>c)
	}
	for c := int16(-0); c < 300; c += 37 {
		println("ci16", c, vi32>>c, vu8<<c)
	}
	// negative shift counts
	neg := []int{-1, -64, -1 << 63}
	for _, n := range neg {
		catch("negl", func() { println(vi64 << n) })
		catch("negr", func() { println(vu8 >> n) })
	}
	var n8 int8 = -1
	catch("neg8", func() { println(vi32 >> n8) })
	var n32 int32 = -2147483648
	catch("neg32", func() { println(vu16 << n32) })
	catch("negassign", func() {
		x := uint32(5)
		x <<= n8
		println(x)
	})
	// shift assignments in loops
	var a uint8 = 1
	var b int16 = 1
	var c int64 = -1
	for i := 0; i < 20; i++ {
		a <<= 1
		a |= uint8(i & 1)
		b = b<<3 | 5
		c >>= 3
		c ^= int64(i) << 60
		println("loop", i, a, b, c)
	}
	// rotate idioms
	for _, s := range []uint{0, 1, 4, 13, 31, 32, 63} {
		r32 := vu32<<(s&31) | vu32>>((32-s)&31)
		r64 := vu64<<(s&63) | vu64>>((64-s)&63)
		println("rot", s, r32, r64)
	}
	// constant shifts and mixed forms
	const k = 1 << 62
	println("const", k>>60, uint64(k)<<1, int64(-1)<<63, ^uint32(0)>>31, int8(-128)>>7, uint8(255)>>7)
	var one = 1
	var f = 1.0
	_ = f
	println("untyped shift", 1<<one, int32(1)<<(one+30), uint8(1)<<(one+6), int64(1)<<(62+one))
	// shift results truncated by conversion
	for s := uint(0); s < 70; s += 9 {
		x := uint64(0xffffffffffffffff) >> s
		println("trunc", s, uint8(x), int8(x), uint16(x), int16(x), uint32(x), int32(x))
	}
	// shift by large uint64 count
	var big uint64 = 1 << 63
	println("big", vi64>>big, vu64<<big, vi8>>big, vi32<<big)
}
