// Integer overflow wraparound for every width: + - * unary minus, ^x,
// &^, increments and compound assignments on narrow types, all on values
// routed through globals/slices so the runtime paths execute (not just the
// constant folder).
package main

var i8s = []int8{0, 1, -1, 127, -128, 126, -127, 0x55, -0x56, 64, -64}
var u8s = []uint8{0, 1, 255, 254, 128, 127, 0x55, 0xaa, 0x80, 0x7f}
var i16s = []int16{0, 1, -1, 32767, -32768, 32766, -32767, 0x5555, 256, -256}
var u16s = []uint16{0, 1, 65535, 65534, 32768, 32767, 0x5555, 0xaaaa, 255}
var i32s = []int32{0, 1, -1, 2147483647, -2147483648, 2147483646, -2147483647, 0x55555555, 65536, -65536}
var u32s = []uint32{0, 1, 4294967295, 4294967294, 2147483648, 2147483647, 0x55555555, 0xaaaaaaaa, 65535}
var i64s = []int64{0, 1, -1, 9223372036854775807, -9223372036854775808, 9223372036854775806, -9223372036854775807, 0x5555555555555555, 1 << 32, -(1 << 32), 3037000500}
var u64s = []uint64{0, 1, 18446744073709551615, 18446744073709551614, 1 << 63, 1<<63 - 1, 0x5555555555555555, 0xaaaaaaaaaaaaaaaa, 1 << 32, 4294967295}

func main() {
	for _, a := range i8s {
		for _, b := range i8s {
			println("i8", a, b, a+b, a-b, a*b, a&^b, a^b, a|b, a&b)
		}
		println("i8u", a, -a, ^a, a+1, a-1)
	}
	for _, a := range u8s {
		for _, b := range u8s {
			println("u8", a, b, a+b, a-b, a*b, a&^b, a^b)
		}
		println("u8u", a, -a, ^a)
	}
	for _, a := range i16s {
		for _, b := range i16s {
			println("i16", a, b, a+b, a-b, a*b, a&^b)
		}
		println("i16u", a, -a, ^a)
	}
	for _, a := range u16s {
		for _, b := range u16s {
			println("u16", a, b, a+b, a-b, a*b)
		}
		println("u16u", a, -a, ^a)
	}
	for _, a := range i32s {
		for _, b := range i32s {
			println("i32", a, b, a+b, a-b, a*b, a&^b)
		}
		println("i32u", a, -a, ^a)
	}
	for _, a := range u32s {
		for _, b := range u32s {
			println("u32", a, b, a+b, a-b, a*b)
		}
		println("u32u", a, -a, ^a)
	}
	for _, a := range i64s {
		for _, b := range i64s {
			println("i64", a, b, a+b, a-b, a*b, a&^b)
		}
		println("i64u", a, -a, ^a)
	}
	for _, a := range u64s {
		for _, b := range u64s {
			println("u64", a, b, a+b, a-b, a*b)
		}
		println("u64u", a, -a, ^a)
	}
	// int / uint / uintptr (64-bit on the oracle platform)
	ints := []int{0, -1, 1<<63 - 1, -1 << 63}
	for _, a := range ints {
		println("int", a, a+1, a-1, a*2, a*a, -a)
	}
	uints := []uint{0, 1, 1<<64 - 1, 1 << 63}
	for _, a := range uints {
		println("uint", a, a+1, a-1, a*3, -a)
	}
	ups := []uintptr{0, 1, ^uintptr(0)}
	for _, a := range ups {
		println("uintptr", a, a+1, a-1, a*7)
	}
	// compound assignments on narrow types in loops
	var c8 int8 = 100
	var cu8 uint8 = 200
	var c16 int16 = 30000
	var cu16 uint16 = 60000
	for i := 0; i < 40; i++ {
		c8 += 7
		cu8 += 13
		c16 += 1234
		cu16 *= 3
		cu16 += 1
		println("cmp", i, c8, cu8, c16, cu16)
	}
	var m8 int8 = 3
	for i := 0; i < 12; i++ {
		m8 *= 3
		m8 -= 1
		println("m8", m8)
	}
	var x8 int8 = 127
	x8++
	println("inc", x8)
	x8--
	println("dec", x8)
	var y8 uint8
	y8--
	println("decu", y8)
	var z32 int32 = -2147483648
	z32--
	println("dec32", z32)
	z32 = -z32
	println("neg32", z32)
	var z64 int64 = -9223372036854775808
	println("neg64", -z64, z64*-1, z64/1)
	// sums that overflow in a loop
	var s32 int32
	var su16 uint16
	var s8 int8
	for i := 0; i < 100000; i++ {
		s32 += int32(i) * 40000
		su16 += uint16(i)
		s8 ^= int8(i) * 31
	}
	println("sums", s32, su16, s8)
	// mixed comparisons via conversion
	m1, h := int8(-1), uint32(1<<31)
	println(m1 < int8(0), uint8(m1) > 0, int32(h) < 0, uint64(int64(m1)) == ^uint64(0), uint8(m1) == 255)
}
