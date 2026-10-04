package main

// Append growth with different element sizes (growth policy is per-byte).
func main() {
	var b []byte
	for i := 0; i < 10; i++ {
		b = append(b, byte(i))
	}
	println("bytes len:", len(b), "cap:", cap(b))

	var w []uint64
	for i := 0; i < 10; i++ {
		w = append(w, uint64(i))
	}
	println("u64 len:", len(w), "cap:", cap(w))
}
