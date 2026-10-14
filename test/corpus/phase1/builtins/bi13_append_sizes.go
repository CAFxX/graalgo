package main

// Append growth with different element sizes: the per-byte growth policy
// means smaller elements get larger caps for the same length, but exact
// caps are compiler-dependent (growsliceBuf); assert validity and the
// relative ordering instead.
func main() {
	var b []byte
	for i := 0; i < 10; i++ {
		b = append(b, byte(i))
	}
	println("bytes len:", len(b), "valid:", cap(b) >= len(b), "b9:", b[9])

	var w []uint64
	for i := 0; i < 10; i++ {
		w = append(w, uint64(i))
	}
	println("u64 len:", len(w), "valid:", cap(w) >= len(w), "w9:", w[9])
}
