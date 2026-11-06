package main

// Nil interface map keys: full lifecycle via builtins.
func main() {
	m := map[any]int{}
	m[nil] = 1
	println("len:", len(m))
	println("lookup:", m[nil])
	v, ok := m[nil]
	println("v:", v, "ok:", ok)
	// overwrite through a nil interface variable: same key
	var k any
	m[k] = 2
	println("len:", len(m), "lookup:", m[nil])
	// range visits the nil key exactly once (order-independent count)
	n := 0
	nils := 0
	sum := 0
	for kk, vv := range m {
		n++
		sum += vv
		if kk == nil {
			nils++
		}
	}
	println("ranged:", n, "nils:", nils, "sum:", sum)
	delete(m, nil)
	println("len:", len(m))
	_, ok = m[nil]
	println("ok:", ok)
	// a typed nil key is a different key from the nil interface key
	var p *int
	m[p] = 3
	println("len:", len(m), "nil-lookup:", m[nil], "typed-lookup:", m[p])
}
