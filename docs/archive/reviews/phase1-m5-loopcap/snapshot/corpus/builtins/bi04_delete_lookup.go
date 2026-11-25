package main

// Delete then lookup: deleted keys are absent; comma-ok is false.
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	delete(m, "b")
	v, ok := m["b"]
	println("b:", v, ok)
	v, ok = m["a"]
	println("a:", v, ok)
	// Delete absent key: no-op, no panic.
	delete(m, "zzz")
	println("len:", len(m))
	// Delete then re-insert.
	m["b"] = 20
	v, ok = m["b"]
	println("b2:", v, ok)
}
