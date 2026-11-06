package main

func main() {
	// nil interface key: insert, lookup, comma-ok
	m := map[any]int{}
	m[nil] = 1
	a := m[nil]
	_, ok := m[nil]
	b := 0
	if ok {
		b = 1
	}
	// overwrite through a nil interface variable: same key
	var k any
	m[k] = 2
	c := m[nil]
	// size and key identity via range (order-independent)
	n := 0
	sum := 0
	nils := 0
	for kk, vv := range m {
		n++
		sum += vv
		if kk == nil {
			nils++
		}
	}
	// a typed nil key is a different key from the nil interface key
	var p *int
	m[p] = 3
	d := m[p]
	e := m[nil]
	n2 := 0
	for kk := range m {
		n2++
		_ = kk
	}
	_ = a
	_ = b
	_ = c
	_ = n
	_ = sum
	_ = nils
	_ = d
	_ = e
	_ = n2
}

// observe: a b c n sum nils d e n2
