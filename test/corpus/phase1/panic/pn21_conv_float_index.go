package main

// B-P1-1 (REVIEW-2026-11-09-13): the index-out-of-range panic message
// embeds the executeLong value of the converted index: gc says
// [-2147483648]; pre-fix graalgo said [-9223372036854775808].
// NaN comes from a runtime 0.0/0.0 division (a variable, not a constant).
func main() {
	var zero float64
	nan := zero / zero
	a := []int{10, 20, 30}
	_ = a[int32(nan)]
}

// panic: panic: runtime error: index out of range [-2147483648]
