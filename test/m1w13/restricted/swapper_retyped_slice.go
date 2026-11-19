// reflectlite.Swapper (sort.Slice) swaps pointer-free elements of size 1, 2,
// 4 or 8 by reinterpreting the slice header as []int8, []int16, []int32 or
// []int64. A slice shares storage, so that needs the elements to be leaves
// of that slot form: any one-byte element, int16, int32, and any single
// 8-byte scalar are (first two lines). A []float32 as []int32 is not — the
// slot of a float32 does not hold an int32 — and neither are []uint16,
// []uint32 or a struct of two int32 as one int64: the fatal error instead
// of a wrong value (SPEC §3.4; docs/arch/limitations.md).
package main

import "sort"

func main() {
	f64 := []float64{3, 1, 2}
	sort.Slice(f64, func(i, j int) bool { return f64[i] < f64[j] })
	println(f64[0], f64[1], f64[2])
	u8 := []uint8{3, 1, 2}
	sort.Slice(u8, func(i, j int) bool { return u8[i] < u8[j] })
	println(u8[0], u8[1], u8[2])
	f32 := []float32{3, 1, 2}
	sort.Slice(f32, func(i, j int) bool { return f32[i] < f32[j] })
	println(f32[0], f32[1], f32[2])
}
