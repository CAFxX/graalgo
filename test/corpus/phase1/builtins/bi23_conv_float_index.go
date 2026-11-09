package main

// B-P1-1 (REVIEW-2026-11-09-13): float->narrow-int conversions consumed
// through executeLong — array index and slice bound. Pre-fix, uint8(300.0)
// read as 300: a[300] is 300 (not 44) and the slice length is 300 (not 44).
// NaN comes from a runtime 0.0/0.0 division (a variable, not a constant).
func main() {
	var zero float64
	nan := zero / zero
	f3e9 := 3e9
	f300 := 300.0
	a := [301]int{}
	a[44] = 44
	a[300] = 300
	println("u8idx:", a[uint8(f300)])
	println("u8len:", len(a[:uint8(f300)]))
	println("cmp:", int32(nan) == -2147483648, int32(f3e9) < 0, int32(nan) == int32(f3e9))
}
