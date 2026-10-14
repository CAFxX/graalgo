package main

// Copy with overlap: memmove semantics (not memcpy).
func main() {
	s := []int{1, 2, 3, 4, 5}
	// Copy forward overlap: copy(s[1:], s[:4]) shifts left.
	n := copy(s[1:], s[:4])
	println("n:", n)
	for _, v := range s {
		print(v, " ")
	}
	println()

	// Copy backward overlap.
	t := []int{1, 2, 3, 4, 5}
	m := copy(t[:4], t[1:])
	println("m:", m)
	for _, v := range t {
		print(v, " ")
	}
	println()

	// Copy truncates to min length.
	d := []int{0, 0}
	k := copy(d, s)
	println("k:", k, "d:", d[0], d[1])
}
