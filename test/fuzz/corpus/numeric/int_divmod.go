// Integer division and modulo for every width: truncation toward zero with
// negative operands, MinInt/-1 (wraps, no panic), x%-1, division by zero
// panics (recovered, message printed), constant divisors (strength-reduced
// by gc) vs variable divisors, and powers of two.
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
		} else if s, ok := r.(string); ok {
			println(name, "panic string:", s)
		} else {
			println(name, "panic other")
		}
	}()
	f()
}

var i8s = []int8{0, 1, -1, 2, -2, 3, -3, 7, -7, 127, -128, 126, -127, 64, -64}
var u8s = []uint8{0, 1, 2, 3, 7, 16, 127, 128, 254, 255}
var i16s = []int16{1, -1, 3, -3, 10, -10, 32767, -32768, 256, -255}
var i32s = []int32{1, -1, 3, -3, 7, -7, 1000, -1000, 2147483647, -2147483648, 65536}
var u32s = []uint32{1, 3, 7, 10, 2147483648, 4294967295, 65535}
var i64s = []int64{1, -1, 3, -3, 7, -7, 10, -10, 9223372036854775807, -9223372036854775808, 1 << 32, 1000000007}
var u64s = []uint64{1, 3, 7, 10, 1 << 63, 18446744073709551615, 1000000007, 4294967296}

//go:noinline
func div8(a, b int8) (int8, int8)     { return a / b, a % b }
func divu8(a, b uint8) (uint8, uint8) { return a / b, a % b }
func div64(a, b int64) (int64, int64) { return a / b, a % b }
func divInt(a, b int) (int, int)      { return a / b, a % b }

func main() {
	for _, a := range i8s {
		for _, b := range i8s {
			if b == 0 {
				continue
			}
			q, r := div8(a, b)
			println("i8", a, b, q, r)
		}
		println("i8c", a, a/2, a%2, a/-2, a%-2, a/3, a%3, a/-1, a%-1, a/16, a%16, a/127)
	}
	for _, a := range u8s {
		for _, b := range u8s {
			if b == 0 {
				continue
			}
			q, r := divu8(a, b)
			println("u8", a, b, q, r)
		}
		println("u8c", a, a/2, a%2, a/3, a%3, a/10, a%10, a/255)
	}
	for _, a := range i16s {
		for _, b := range i16s {
			println("i16", a, b, a/b, a%b)
		}
	}
	for _, a := range i32s {
		for _, b := range i32s {
			println("i32", a, b, a/b, a%b)
		}
		println("i32c", a, a/7, a%7, a/-7, a%-7, a/1024, a%1024, a/-1)
	}
	for _, a := range u32s {
		for _, b := range u32s {
			println("u32", a, b, a/b, a%b)
		}
		println("u32c", a, a/7, a%7, a/1024, a%1024)
	}
	for _, a := range i64s {
		for _, b := range i64s {
			q, r := div64(a, b)
			println("i64", a, b, q, r)
		}
		println("i64c", a, a/7, a%7, a/-7, a%-7, a/1000000007, a%1000000007, a/-1, a%-1, a/(1<<20))
	}
	for _, a := range u64s {
		for _, b := range u64s {
			println("u64", a, b, a/b, a%b)
		}
		println("u64c", a, a/7, a%7, a/10, a%10, a/1000000007)
	}
	q, r := divInt(-1<<63, -1)
	println("minint/-1", q, r)
	var m8 int8 = -128
	var n1 int8 = -1
	println("min8/-1", m8/n1, m8%n1)
	var m16 int16 = -32768
	var n16 int16 = -1
	println("min16/-1", m16/n16, m16%n16)
	var m32 int32 = -2147483648
	var n32 int32 = -1
	println("min32/-1", m32/n32, m32%n32)
	m8 /= n1
	println("min8/=-1", m8)

	var z8 int8
	var zu8 uint8
	var z32 int32
	var z64 int64
	var zu64 uint64
	var z int
	var zu uintptr
	catch("div8", func() { println(i8s[3] / z8) })
	catch("mod8", func() { println(i8s[3] % z8) })
	catch("divu8", func() { println(u8s[3] / zu8) })
	catch("div32", func() { println(i32s[3] / z32) })
	catch("mod32", func() { println(i32s[3] % z32) })
	catch("div64", func() { println(i64s[3] / z64) })
	catch("divu64", func() { println(u64s[3] % zu64) })
	catch("divint", func() { divInt(5, z) })
	catch("divuintptr", func() { println(uintptr(9) / zu) })
	catch("divassign", func() {
		x := int16(77)
		x /= int16(z)
		println(x)
	})
	catch("modassign", func() {
		x := uint32(77)
		x %= uint32(z)
		println(x)
	})
	catch("min/-1 ok", func() { divInt(-1<<63, int(n1)) })
	// sign identities over a range
	bad := 0
	for a := -300; a <= 300; a++ {
		for b := -17; b <= 17; b++ {
			if b == 0 {
				continue
			}
			if (a/b)*b+a%b != a {
				bad++
			}
			x := int8(a)
			y := int8(b)
			if (x/y)*y+x%y != x {
				bad++
			}
		}
	}
	println("identity bad", bad)
}
