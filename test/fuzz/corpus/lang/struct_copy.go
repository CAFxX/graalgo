// Targets struct copy semantics in a flat value model: structs containing
// arrays and nested structs assigned, passed, returned, stored in arrays,
// slices, maps and interfaces, then mutated; the original must not change.
package main

type Inner struct {
	A [4]int
	S string
}

type Outer struct {
	In   Inner
	Arr  [2]Inner
	P    *Inner
	Sl   []int
	Flag bool
}

//go:noinline
func mutate(o Outer) Outer {
	o.In.A[0] = 100
	o.Arr[1].A[3] = 200
	o.Arr[0].S = "changed"
	o.P.A[1] = 300 // shared
	o.Sl[0] = 400  // shared
	o.Flag = !o.Flag
	return o
}

//go:noinline
func mutatePtr(o *Outer) {
	o.In.A[2] = -1
	o.Arr[0] = Inner{S: "replaced"}
}

func show(tag string, o Outer) {
	println(tag, o.In.A[0], o.In.A[1], o.In.A[2], o.In.A[3], o.In.S,
		o.Arr[0].A[0], o.Arr[0].S, o.Arr[1].A[3], o.Arr[1].S,
		o.P.A[1], o.Sl[0], o.Flag)
}

var global Outer

func getGlobal() Outer { return global }

func main() {
	shared := &Inner{S: "shared"}
	o := Outer{
		In:  Inner{A: [4]int{1, 2, 3, 4}, S: "in"},
		Arr: [2]Inner{{A: [4]int{5}, S: "a0"}, {A: [4]int{0, 0, 0, 6}, S: "a1"}},
		P:   shared,
		Sl:  []int{7, 8},
	}
	show("orig", o)
	r := mutate(o)
	show("orig'", o)
	show("ret", r)

	// assignment copy
	c := o
	c.In.A[3] = 44
	c.Arr[1].S = "c-a1"
	show("o", o)
	show("c", c)

	// array of structs: copy whole array
	arr := [3]Outer{o, c, r}
	arr2 := arr
	arr2[0].In.A[0] = 999
	arr2[1].Arr[0].A[0] = 888
	println(arr[0].In.A[0], arr2[0].In.A[0], arr[1].Arr[0].A[0], arr2[1].Arr[0].A[0])

	// range copies the element
	for i, e := range arr {
		e.In.S = "range"
		e.In.A[1] = i * 10
		println(i, arr[i].In.S, e.In.S, arr[i].In.A[1], e.In.A[1])
	}

	// range over pointer to array: element still copied
	for i, e := range &arr {
		if i == 0 {
			arr[2].In.S = "mutated-during-range"
		}
		println(i, e.In.S)
	}

	// map values are copies
	m := map[string]Inner{"k": o.In}
	v := m["k"]
	v.A[0] = -5
	println(m["k"].A[0], v.A[0])
	m["k"] = v
	println(m["k"].A[0])

	// interface boxing copies
	var x any = o.In
	o.In.A[0] = 12345
	println(x.(Inner).A[0], o.In.A[0])
	y := x.(Inner)
	y.A[0] = 7
	println(x.(Inner).A[0], y.A[0])

	// pointer mutation
	mutatePtr(&o)
	show("ptr", o)
	show("c-after", c)

	// global struct copies
	global = o
	g := getGlobal()
	g.In.A[0] = 31337
	println(global.In.A[0], g.In.A[0])
	global.Arr[1].A[2] = 77
	println(g.Arr[1].A[2], global.Arr[1].A[2])

	// slice of structs: element address vs copy
	sl := []Inner{{S: "s0"}, {S: "s1"}}
	p0 := &sl[0]
	cp := sl[1]
	p0.A[0] = 1
	cp.A[0] = 2
	println(sl[0].A[0], sl[1].A[0], cp.A[0])
	sl = append(sl, Inner{S: "s2"}) // reallocates
	p0.A[0] = 50
	println(sl[0].A[0], p0.A[0], len(sl))

	// copy() of struct slice
	dst := make([]Inner, 2)
	n := copy(dst, sl)
	dst[0].A[0] = 66
	println(n, sl[0].A[0], dst[0].A[0], dst[1].S)

	// swap of struct values
	a, b := Inner{S: "a", A: [4]int{1}}, Inner{S: "b", A: [4]int{2}}
	a, b = b, a
	println(a.S, a.A[0], b.S, b.A[0])
	arr[0].In, arr[1].In = arr[1].In, arr[0].In
	println(arr[0].In.A[3], arr[1].In.A[3])

	// nested array of arrays
	var grid [3][3]int
	row := grid[1]
	row[1] = 5
	grid[2] = row
	grid[2][0] = 9
	println(grid[1][1], grid[2][1], grid[2][0], row[0])

	// struct returned from func then indexed
	println(makeInner(3).A[3], makeInner(4).S)
}

//go:noinline
func makeInner(k int) Inner {
	var in Inner
	for i := range in.A {
		in.A[i] = i * k
	}
	in.S = string(rune('a' + k))
	return in
}
