package main

// Append aliasing: append must not mutate the input slice's backing array
// beyond its length; the result shares or copies correctly.
func main() {
	s := []int{1, 2, 3}
	t := append(s, 4)
	// s must be unchanged.
	println("s:", s[0], s[1], s[2], "len:", len(s))
	println("t:", t[0], t[1], t[2], t[3], "len:", len(t))
	// Mutating t beyond len(s) must not affect s's elements.
	t[0] = 99
	println("s0:", s[0], "t0:", t[0])
}
