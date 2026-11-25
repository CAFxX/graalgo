package main

func main() {
	// map literal, index, assign, range
	m := map[string]int{"a": 1, "b": 2}
	m["c"] = 3
	a := m["a"] + m["b"] + m["c"]
	// overwrite
	m["a"] = 10
	b := m["a"]
	// keyed int map
	n := map[int]int{1: 100, 2: 200}
	c := n[1] + n[2]
	// range and sum (order-independent)
	ks := 0
	vs := 0
	for k, v := range n {
		ks += k
		vs += v
	}
	// map value semantics: assignment shares the map
	p := m
	p["a"] = 99
	d := m["a"]
	_ = a
	_ = b
	_ = c
	_ = ks
	_ = vs
	_ = d
}

// observe: a b c ks vs d
