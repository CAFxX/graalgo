// 60_narrow_unary_widen: unary -x and ^x on sub-64-bit integers whose
// result is consumed WIDENED (converted to int/int64/uint64, used as an
// index or map key, passed as interface) rather than stored back at the
// narrow width; likewise narrowing conversions (int32(x)) consumed widened. The wraparound (-int8(-128) == -128, ^uint8(0) == 255)
// must happen at the operand's width before widening.
package main

import "fmt"

//go:noinline
func id8(v int8) int8 { return v }

func main() {
	a8, a16, a32 := id8(-128), int16(-32768), int32(-2147483648)
	u8, u16, u32 := uint8(1), uint16(1), uint32(1)
	fmt.Println("neg-widen", int64(-a8), int64(-a16), int64(-a32), uint64(-u8), uint64(-u16), uint64(-u32))
	fmt.Println("not-widen", int64(^a8), int64(^a16), int64(^a32), uint64(^u8), uint64(^u16), uint64(^u32))
	fmt.Println("as-int", int(-a8)+1, int(^u8)+1, int(-u16)%7, int(-u32)/3)
	table := make([]int, 256)
	for i := range table {
		table[i] = i * 3
	}
	z := uint8(0)
	fmt.Println("index", table[^z], table[-u8], table[uint8(-a8)])
	m := map[int64]string{-128: "wrapped", 128: "not wrapped"}
	fmt.Println("mapkey", m[int64(-a8)])
	var anys []any
	anys = append(anys, -a8, ^u8, -u16, -a32, ^u32)
	fmt.Println(append([]any{"iface"}, anys...)...)
	fmt.Printf("%d %d %d\n", -a8, -u8, ^a16)
	// narrowing conversions consumed widened
	big := int64(0x1_8000_0001)
	fmt.Println("conv-widen", int64(int32(big)), int64(int16(big)), int64(int8(big)), uint64(uint32(big)), int64(int32(big))+1)
	fmt.Println("conv-index", table[uint8(int32(big))], int(int32(big))%1000)
	b := -a8 < 0
	c := -u8 > 200
	fmt.Println("cmp", b, c, -a32 < 0, ^u32 == 4294967295)
}
