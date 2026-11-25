package main

func main() {
	m := map[string]int{"a": 1, "b": 2}
	// comma-ok on present key
	v, ok := m["a"]
	a := v
	b := 0
	if ok {
		b = 1
	}
	// comma-ok on missing key
	v2, ok2 := m["zzz"]
	c := v2
	d := 0
	if !ok2 {
		d = 1
	}
	// comma-ok with plain assignment
	var e int
	var f bool
	e, f = m["b"]
	fi := 0
	if f {
		fi = 1
	}
	// comma-ok with blank value
	_, ok3 := m["c"]
	g := 0
	if !ok3 {
		g = 1
	}
	_ = a
	_ = b
	_ = c
	_ = d
	_ = e
	_ = fi
	_ = g
}

// observe: a b c d e fi g
