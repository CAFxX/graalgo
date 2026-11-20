// Conversions between every integer width and signedness (truncation, sign
// extension, zero extension), chained conversions, conversions in
// comparisons, and integer<->string(rune) conversions incl. invalid runes.
package main

var src = []int64{0, 1, -1, 127, 128, -128, -129, 255, 256, 32767, 32768, -32768, -32769, 65535, 65536,
	2147483647, 2147483648, -2147483648, -2147483649, 4294967295, 4294967296,
	9223372036854775807, -9223372036854775808, 0x5555555555555555, -0x5555555555555555, 0x1234567890abcdef}

var usrc = []uint64{0, 1, 0x80, 0xff, 0x8000, 0xffff, 0x80000000, 0xffffffff, 0x8000000000000000, 0xffffffffffffffff, 0xdeadbeefcafebabe}

func toI8(x int64) int8       { return int8(x) }
func toU8(x int64) uint8      { return uint8(x) }
func toI16(x int64) int16     { return int16(x) }
func toU16(x int64) uint16    { return uint16(x) }
func toI32(x int64) int32     { return int32(x) }
func toU32(x int64) uint32    { return uint32(x) }
func toU64(x int64) uint64    { return uint64(x) }
func toUptr(x int64) uintptr  { return uintptr(x) }
func fromU(x uint64) int64    { return int64(x) }
func i8ToU64(x int8) uint64   { return uint64(x) }
func u8ToI64(x uint8) int64   { return int64(x) }
func i16ToU32(x int16) uint32 { return uint32(x) }
func u32ToI16(x uint32) int16 { return int16(x) }

func main() {
	for _, x := range src {
		println("from", x, toI8(x), toU8(x), toI16(x), toU16(x), toI32(x), toU32(x), toU64(x), toUptr(x), int(x), uint(x))
		a := toI8(x)
		b := toU8(x)
		c := toI16(x)
		d := toU32(x)
		println("  chain", i8ToU64(a), u8ToI64(b), i16ToU32(c), u32ToI16(d), int64(int32(d)), uint64(uint16(c)), int16(int8(c)), uint8(int8(b)))
		println("  cmp", int8(x) < 0, uint8(x) > 127, int32(x) == int32(uint32(x)), int64(uint32(x)) == x, uint64(int32(x)) == uint64(x))
	}
	for _, u := range usrc {
		println("ufrom", u, fromU(u), int8(u), uint8(u), int16(u), uint16(u), int32(u), uint32(u), int(u))
	}
	// conversions inside arithmetic
	var b uint8 = 200
	var c int8 = -56
	println("mix", int16(b)+int16(c), uint8(c)+b, int(b)*int(c), uint16(c)*uint16(b), int32(int8(b))*2)
	// byte/rune conversions to string
	runes := []int64{0x41, 0xe9, 0x4e16, 0x1f600, 0x10ffff, 0x110000, 0xd800, 0xdfff, -1, 0x7fffffff, 1 << 32, 'A' + 1<<32}
	for _, r := range runes {
		s := string(rune(r))
		println("rune", r, s, len(s), []byte(s)[0])
	}
	bs := []int{65, 255, 256, 321, -191}
	for _, x := range bs {
		s := string(rune(byte(x)))
		println("byte", x, byte(x), s, len(s))
	}
	// rune slices
	rs := []rune("héllo, 世界\xff!")
	for i, r := range rs {
		println("rs", i, r, int32(r) == r, uint16(r), int8(r))
	}
	println(string(rs), len(string(rs)))
	// sign extension through loops
	var acc uint64
	for i := -300; i <= 300; i += 7 {
		acc = acc*31 + uint64(int8(i)) + uint64(uint8(i))<<8 + uint64(int16(i*301))<<16
	}
	println("acc", acc)
	// large uint64 -> signed and back in comparisons
	h := uint64(1) << 63
	println("hi", int64(h) < 0, int64(h-1) > 0, uint32(h>>32) == 1<<31, int32(uint32(h>>32)) < 0)
	// int <-> uint
	var mi int = -1
	println("mi", uint(mi), uint32(mi), uint8(mi), uintptr(mi) == ^uintptr(0))
}
