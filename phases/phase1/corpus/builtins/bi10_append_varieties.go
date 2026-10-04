package main

// Append to nil slice; append multiple; append string to bytes.
// (Exact caps are not asserted: gc's growsliceBuf optimization makes them
// compiler-dependent; validity is checked in bi01.)
func main() {
	var s []int
	s = append(s, 1, 2, 3)
	println("len:", len(s), "s:", s[0], s[1], s[2])

	t := append(s, 4, 5, 6, 7, 8)
	println("t len:", len(t), "t:", t[0], t[7])

	// Append string to []byte.
	b := []byte("hi")
	b = append(b, "!!"...)
	println("bytes:", string(b))
}
