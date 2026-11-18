// 49_int_arith_edges: Go integer arithmetic at every width — signed and
// unsigned / and % with negative operands (truncation toward zero, the
// dividend's sign), MinInt/-1, wraparound of + - * and unary minus,
// ++/-- wrap, compound assignment. Kills lowering mutants such as signed
// % -> Math.floorMod, / -> floorDiv, dropped width truncation, ADD<->SUB.
package main

import "fmt"

//go:noinline
func i8(v int64) int8 { return int8(v) }

func main() {
	ns := []int64{-7, 7, -8, 8, -1, 1, 0, 9223372036854775807, -9223372036854775808}
	ds := []int64{3, -3, 2, -2, 1, -1, 7, -9}
	for _, n := range ns {
		for _, d := range ds {
			fmt.Println("i64", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []int32{-7, 7, -2147483648, 2147483647, -100} {
		for _, d := range []int32{3, -3, -1, 16, -16} {
			fmt.Println("i32", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []int16{-7, 7, -32768, 32767, -300} {
		for _, d := range []int16{3, -3, -1, 256} {
			fmt.Println("i16", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []int8{-7, 7, -128, 127, -100} {
		for _, d := range []int8{3, -3, -1, 2} {
			fmt.Println("i8", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []uint64{7, 18446744073709551615, 9223372036854775808, 0} {
		for _, d := range []uint64{3, 2, 18446744073709551615, 10} {
			fmt.Println("u64", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []uint32{7, 4294967295, 2147483648} {
		for _, d := range []uint32{3, 4294967295, 16} {
			fmt.Println("u32", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []uint16{7, 65535, 40000} {
		for _, d := range []uint16{3, 65535, 300} {
			fmt.Println("u16", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	for _, n := range []uint8{7, 255, 200} {
		for _, d := range []uint8{3, 255, 100} {
			fmt.Println("u8", n, d, n/d, n%d, n+d, n-d, n*d)
		}
	}
	// unary minus and ^ at the boundaries
	a8, a16, a32, a64 := int8(-128), int16(-32768), int32(-2147483648), int64(-9223372036854775808)
	fmt.Println("neg", -a8, -a16, -a32, -a64, ^a8, ^a16, ^a32, ^a64)
	u8, u16, u32, u64 := uint8(1), uint16(1), uint32(1), uint64(1)
	fmt.Println("uneg", -u8, -u16, -u32, -u64, ^u8, ^u16, ^u32, ^u64)
	// ++/-- wrap
	x8 := int8(127)
	x8++
	y8 := uint8(0)
	y8--
	x32 := int32(-2147483648)
	x32--
	fmt.Println("incdec", x8, y8, x32)
	// compound assignment, mixed
	c := int16(1000)
	c *= 100
	c += 30000
	c -= -5
	c /= 3
	c %= 77
	fmt.Println("compound", c)
	acc := uint32(2166136261)
	for i := 0; i < 50; i++ {
		acc ^= uint32(i)
		acc *= 16777619
	}
	fmt.Println("fnv", acc)
	fmt.Println("i8fn", i8(200), i8(-129), i8(1<<40+5))
	// int is 64-bit
	m := -9223372036854775807 - 1
	fmt.Println("int", m/-1, m%-1, m*-1, m-1)
}
