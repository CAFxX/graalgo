package main

// Append growth: capacities must be valid (cap >= len, non-decreasing).
// Exact cap values are a gc compiler implementation detail (the
// growsliceBuf stack-buffer optimization changes observable caps vs the
// pure runtime growslice algorithm) and are not specified by the Go
// language. The growth algorithm itself is pinned by unit tests against
// the Go source (runtime/slice.go nextslicecap).
func main() {
	var s []int
	prevCap := 0
	valid := true
	for i := 0; i < 20; i++ {
		s = append(s, i)
		if cap(s) < len(s) || cap(s) < prevCap {
			valid = false
		}
		prevCap = cap(s)
	}
	println("valid:", valid, "len:", len(s))
	// Sum to verify elements.
	sum := 0
	for _, v := range s {
		sum += v
	}
	println("sum:", sum)
}
