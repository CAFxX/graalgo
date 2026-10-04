package main

// Append to nil slice; append multiple; append string to bytes.
func main() {
	var s []int
	s = append(s, 1, 2, 3)
	println("len:", len(s), "cap:", cap(s))

	t := append(s, 4, 5, 6, 7, 8)
	println("t len:", len(t), "cap:", cap(t))

	// Append string to []byte.
	b := []byte("hi")
	b = append(b, "!!"...)
	println("bytes:", string(b))
}
