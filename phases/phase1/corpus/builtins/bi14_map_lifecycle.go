package main

// Map lifecycle: make, assign, len, delete, lookup.
func main() {
	m := make(map[int]string)
	m[1] = "one"
	m[2] = "two"
	println("len:", len(m))
	delete(m, 1)
	println("len:", len(m))
	v, ok := m[1]
	println("v:", v, "ok:", ok)
	v, ok = m[2]
	println("v:", v, "ok:", ok)
}
