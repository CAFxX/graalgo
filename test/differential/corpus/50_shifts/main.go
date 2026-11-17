// 50_shifts: Go shift semantics (never Java's count masking): counts
// >= width give 0 for << and unsigned >>, sign fill for signed >>; counts
// of every unsigned and signed type; the negative-shift-count panic.
package main

import "fmt"

func main() {
	counts := []uint{0, 1, 7, 8, 15, 16, 31, 32, 33, 63, 64, 65, 200}
	for _, s := range counts {
		fmt.Println("i64", s, int64(-12345)<<s, int64(-12345)>>s, int64(12345)>>s, int64(1)<<s)
		fmt.Println("u64", s, uint64(0xF0F0F0F0F0F0F0F0)<<s, uint64(0xF0F0F0F0F0F0F0F0)>>s)
		fmt.Println("i32", s, int32(-12345)<<s, int32(-12345)>>s, int32(0x40000000)<<s)
		fmt.Println("u32", s, uint32(0xF0F0F0F0)<<s, uint32(0xF0F0F0F0)>>s)
		fmt.Println("i16", s, int16(-1234)<<s, int16(-1234)>>s, int16(0x4000)<<s)
		fmt.Println("u16", s, uint16(0xF0F0)<<s, uint16(0xF0F0)>>s)
		fmt.Println("i8", s, int8(-100)<<s, int8(-100)>>s, int8(0x40)<<s)
		fmt.Println("u8", s, uint8(0xF5)<<s, uint8(0xF5)>>s)
		fmt.Println("int", s, -1<<s, -7>>s)
	}
	// count types other than uint
	var c8 uint8 = 70
	var c16 int16 = 3
	var c32 int32 = 40
	var c64 uint64 = 1 << 40
	fmt.Println("ctypes", 1<<c8, 1024>>c16, int64(-1)<<c32, uint64(1)<<c64, 5<<c16)
	// shifted-in bits and masking interplay
	v := uint8(0x81)
	fmt.Println("mix", v<<1, v>>1, int8(v)>>1, uint16(v)<<9, (v<<4)>>4)
	defer func() { fmt.Println("recovered:", recover()) }()
	n := -1
	if len(counts) > 0 {
		n = -len(counts)
	}
	fmt.Println(1 << n)
}
