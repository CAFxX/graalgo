// Targets range semantics: range over an array copies it, range over a
// pointer-to-array does not, range over a slice evaluates the header once
// (appends ignored, element writes visible), per-iteration loop vars, clear().
package main

type item struct {
	n   int
	tag string
}

var calls int

func getSlice(s []int) []int {
	calls++
	return s
}

func main() {
	arr := [5]int{1, 2, 3, 4, 5}
	for i, v := range arr {
		if i == 0 {
			arr[4] = 500
			arr[1] = 200
		}
		println("arrcopy", i, v)
	}
	println("arr after", arr[1], arr[4])
	arr = [5]int{1, 2, 3, 4, 5}
	for i, v := range &arr {
		if i == 0 {
			arr[4] = 500
			arr[1] = 200
		}
		println("arrptr", i, v)
	}
	arr = [5]int{1, 2, 3, 4, 5}
	for i, v := range arr[:] {
		if i == 0 {
			arr[4] = 500
		}
		println("arrslice", i, v)
	}
	// slice: elements visible, appends not
	s := []int{1, 2, 3}
	for i, v := range s {
		s = append(s, v*10)
		if i == 0 {
			s[2] = 33
		}
		println("slice", i, v, len(s))
	}
	// first append reallocated; s[2]=33 wrote the new array
	s2 := make([]int, 3, 10)
	s2[0], s2[1], s2[2] = 1, 2, 3
	for i, v := range s2 {
		s2 = append(s2, v)
		s2[2] = 99
		println("slicecap", i, v)
	}
	// range expr evaluated once
	calls = 0
	for range getSlice([]int{1, 2, 3, 4}) {
	}
	println("calls", calls)
	// shrinking slice in loop does not stop it
	t := []int{1, 2, 3, 4, 5}
	for i, v := range t {
		t = t[:1]
		println("shrink", i, v, len(t))
	}
	// range with only index over nil and empty
	var nilS []int
	n := 0
	for range nilS {
		n++
	}
	for range [0]int{} {
		n++
	}
	println("nilrange", n)
	// per-iteration loop variables captured by closures
	var fs []func() int
	for i, v := range []int{10, 20, 30} {
		fs = append(fs, func() int { return i*100 + v })
	}
	for _, f := range fs {
		println("closure", f())
	}
	var gs []func() int
	for i := 0; i < 3; i++ {
		gs = append(gs, func() int { return i })
	}
	for _, g := range gs {
		println("forclosure", g())
	}
	// mutating struct copies vs index
	items := []item{{1, "a"}, {2, "b"}, {3, "c"}}
	for _, it := range items {
		it.n *= 10
	}
	println("structcopy", items[0].n, items[2].n)
	for i := range items {
		items[i].n *= 10
		items[i].tag += "!"
	}
	println("structidx", items[0].n, items[2].n, items[1].tag)
	p := &items[1]
	items[1].n = 7
	println("ptr", p.n)
	items = append(items, item{4, "d"})
	items[1].n = 8
	println("ptrstale", p.n)
	// range over array of arrays: copies inner too
	aa := [2][2]int{{1, 2}, {3, 4}}
	for _, row := range aa {
		row[0] = 0
	}
	println("aa", aa[0][0], aa[1][0])
	// clear on slice and map
	cs := []int{1, 2, 3, 4, 5}
	clear(cs[1:3])
	for _, v := range cs {
		print(v, " ")
	}
	println(len(cs))
	cstr := []string{"x", "y"}
	clear(cstr)
	println("clearstr", cstr[0] == "", len(cstr))
	cst := []item{{1, "a"}}
	clear(cst)
	println("clearstruct", cst[0].n, cst[0].tag == "")
	m := map[string]int{"a": 1, "b": 2}
	clear(m)
	println("clearmap", len(m))
	m["c"] = 3
	println("afterclear", len(m), m["c"])
	var nm map[int]int
	clear(nm)
	clear(nilS)
	println("clearnil ok", len(nm))
	// range over integer
	sum := 0
	for i := range 10 {
		sum += i
	}
	println("rangeint", sum)
	var k int64 = 5
	for i := range k {
		print(i, " ")
	}
	println()
	// range over string reslicing inside
	str := "héllo"
	cnt := 0
	for i := range str {
		str = str[1:]
		cnt += i
	}
	println("strrange", cnt, str)
	// labeled continue in nested range
	total := 0
outer:
	for i, row := range [][]int{{1, 2, 3}, {4, -1, 6}, {7, 8, 9}} {
		for _, v := range row {
			if v < 0 {
				continue outer
			}
			total += v * (i + 1)
		}
	}
	println("labeled", total)
	// assignment to index var in loop doesn't affect iteration
	for i := range 3 {
		i += 10
		print(i, " ")
	}
	println()
	// range over array value returned from func
	mk := func() [3]string { return [3]string{"p", "q", "r"} }
	for i, v := range mk() {
		print(i, v, " ")
	}
	println()
}
