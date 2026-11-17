// 51_int_conversions: conversions between every integer width
// (truncation, sign/zero extension), float<->int (truncation toward zero,
// in-range values), float64<->float32 rounding, integer->string(rune),
// string<->[]byte/[]rune including invalid UTF-8.
package main

import "fmt"

func main() {
	src := []int64{-1, 255, 256, -129, 128, 65535, -32769, 2147483648, -2147483649, 0x1234567890ABCDEF, -0x7FFFFFFFFFFFFFFF}
	for _, v := range src {
		fmt.Println("from", v, int8(v), uint8(v), int16(v), uint16(v), int32(v), uint32(v), uint64(v), int(v), uintptr(v))
	}
	for _, v := range []uint64{18446744073709551615, 9223372036854775808, 4294967296} {
		fmt.Println("ufrom", v, int64(v), int32(v), int8(v), uint8(v), float64(v), float32(v))
	}
	for _, f := range []float64{1.9, -1.9, 0.5, -0.5, 2147483647.7, -2147483648.9, 1e15 + 0.5, -0.0} {
		fmt.Println("ftoi", f, int64(f), int32(f), int(f), uint8(int(f)))
	}
	for _, f := range []float64{1.5, 255.99, 3e9, 1.8e19} {
		fmt.Println("ftou", f, uint64(f), uint32(f))
	}
	for _, i := range []int64{1 << 53, 1<<53 + 1, -(1 << 62) - 1, 123456789} {
		fmt.Println("itof", i, float64(i), float32(i))
	}
	for _, f := range []float64{0.1, 1.0000001, 3.4028235e38, 1e-46, 16777217} {
		fmt.Println("f64to32", f, float32(f), float64(float32(f)))
	}
	for _, r := range []int{65, 0x1F600, -1, 0xD800, 0x10FFFF, 0x110000} {
		fmt.Printf("rune %d %q %d\n", r, string(rune(r)), len(string(rune(r))))
	}
	s := "h\xffé\xe2\x82"
	b := []byte(s)
	rs := []rune(s)
	fmt.Println("bytes", len(b), b, "runes", len(rs), rs, string(rs) == s, string(b) == s)
	for i, r := range s {
		fmt.Println("range", i, r)
	}
	b[0] = 'H'
	fmt.Println("copy", s, string(b))
	fmt.Println("byte+rune", byte('a'+1), rune('z'-1), string(rune('a'+2)))
}
