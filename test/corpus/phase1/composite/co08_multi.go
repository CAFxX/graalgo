package main

func main() {
	// multi-dimensional slice (elided inner literals)
	m := [][]int{{1, 2}, {3, 4, 5}}
	a := m[0][1] + m[1][2]
	// nested struct in slice
	type rec struct {
		v int
	}
	rs := []rec{{1}, {2}, {3}}
	b := rs[0].v + rs[1].v + rs[2].v
	// map of slices
	ms := map[string][]int{"x": {1, 2}, "y": {3}}
	c := ms["x"][1] + ms["y"][0]
	// slice of maps
	sm := []map[string]int{{"a": 1}, {"b": 2}}
	d := sm[0]["a"] + sm[1]["b"]
	// 2-d array
	var arr [2][3]int
	arr[1][2] = 42
	e := arr[1][2] + arr[0][0]
	_ = a
	_ = b
	_ = c
	_ = d
	_ = e
}

// observe: a b c d e
