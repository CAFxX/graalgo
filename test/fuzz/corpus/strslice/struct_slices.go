// Targets slices/arrays/maps of structs: mutation through index vs copies,
// pointers into a slice surviving reallocation (stale aliasing), arrays as
// map values (copy on read), nested slices in structs and struct equality.
package main

type inner struct {
	vals [3]int
	name string
}

type outer struct {
	in   inner
	list []int
	arr  [2]inner
}

type pair struct{ a, b int }

//go:noinline
func bump(o outer) outer {
	o.in.vals[0]++
	o.list[0]++ // shared backing array
	o.arr[1].name += "!"
	return o
}

//go:noinline
func bumpPtr(o *outer) {
	o.in.vals[0] += 10
	o.list = append(o.list, 99)
	o.arr[0].vals[2] = 7
}

func main() {
	o := outer{in: inner{name: "i"}, list: []int{1, 2}, arr: [2]inner{{name: "a0"}, {name: "a1"}}}
	o2 := bump(o)
	println("bump", o.in.vals[0], o2.in.vals[0], o.list[0], o2.list[0], o.arr[1].name, o2.arr[1].name)
	bumpPtr(&o)
	println("bumpptr", o.in.vals[0], len(o.list), o.arr[0].vals[2], len(o2.list))
	// slices of structs
	ps := make([]pair, 4)
	for i := range ps {
		ps[i].a = i
		ps[i].b = i * i
	}
	for _, p := range ps {
		p.a = -1
	}
	sum := 0
	for _, p := range ps {
		sum += p.a + p.b
	}
	println("pairsum", sum)
	first := &ps[0]
	first.b = 100
	println("ptrinto", ps[0].b)
	ps = append(ps, pair{9, 9}) // cap 4 -> realloc
	first.b = 200
	println("stale", ps[0].b, first.b, len(ps), cap(ps))
	ps2 := ps[1:3]
	ps2[0].a = 55
	println("subslice", ps[1].a)
	// arrays as map values are copied on read
	am := map[string][3]int{"x": {1, 2, 3}}
	v := am["x"]
	v[0] = 100
	println("mapcopy", am["x"][0], v[0])
	am["x"] = v
	println("mapstore", am["x"][0])
	sm := map[int]pair{1: {1, 1}}
	p := sm[1]
	p.a = 50
	println("mapstruct", sm[1].a)
	sm[1] = p
	println("mapstruct2", sm[1].a)
	// struct equality including arrays and strings
	x := inner{vals: [3]int{1, 2, 3}, name: "n"}
	y := x
	println("eq", x == y)
	y.vals[2] = 4
	println("eq2", x == y)
	y.vals[2] = 3
	y.name = "n" + string(rune('a' - 'a' + 'n'))[:0]
	println("eq3", x == y)
	// struct map keys built from arrays
	km := map[inner]int{}
	km[x] = 1
	km[inner{vals: [3]int{1, 2, 3}, name: "n"}]++
	println("structkey", km[x], len(km))
	// slices of slices: row aliasing
	grid := make([][]int, 3)
	backing := make([]int, 9)
	for i := range grid {
		grid[i] = backing[i*3 : i*3+3]
	}
	grid[1][2] = 5
	println("backing", backing[5])
	grid[0] = append(grid[0], 42) // overwrites grid[1][0]
	println("rowclobber", grid[1][0], backing[3])
	grid[2] = append(grid[2], 43) // beyond cap -> new
	grid[2][0] = -9
	println("rownew", backing[6], grid[2][0], len(grid[2]))
	// arrays of slices: copying the array shares slices
	var as [2][]int
	as[0] = []int{1, 2}
	as2 := as
	as2[0][0] = 100
	as2[1] = []int{3}
	println("arrslices", as[0][0], as[1] == nil, len(as2[1]))
	// swap via index of structs
	ps[0], ps[1] = ps[1], ps[0]
	println("swap", ps[0].a, ps[1].a, ps[0].b)
	// struct with embedded array copy in loop of pointers
	ptrs := []*pair{}
	for i := range 3 {
		pp := pair{i, -i}
		ptrs = append(ptrs, &pp)
	}
	for _, q := range ptrs {
		q.a *= 10
	}
	println("ptrs", ptrs[0].a, ptrs[1].a, ptrs[2].a, ptrs[2].b)
	// multi assignment ordering with index expressions
	arr := []int{0, 1, 2}
	i := 0
	i, arr[i] = 1, 9
	println("order", i, arr[0], arr[1])
	arr[0], arr[arr[0]%3] = 5, 6
	println("order2", arr[0], arr[1], arr[2])
	// large slice of structs zero-init and sum
	big := make([]outer, 2000)
	total := 0
	for k := range big {
		big[k].in.vals[k%3] = k
		big[k].arr[k%2].vals[0] = 1
	}
	for _, b := range big {
		total += b.in.vals[0] + b.in.vals[1] + b.in.vals[2] + b.arr[0].vals[0] + len(b.list)
	}
	println("bigstruct", total)
}
