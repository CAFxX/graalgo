package main

// Make with len/cap: exact lengths, capacities, and zero values.
func main() {
	s := make([]int, 3, 5)
	println("len:", len(s), "cap:", cap(s))
	println("elems:", s[0], s[1], s[2])

	s2 := make([]int, 4)
	println("len:", len(s2), "cap:", cap(s2))

	m := make(map[string]int)
	m["x"] = 1
	println("map len:", len(m))

	m2 := make(map[string]int, 100)
	println("map2 len:", len(m2))
}
