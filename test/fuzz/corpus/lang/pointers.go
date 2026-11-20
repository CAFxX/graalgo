// Targets pointers into the flat value model: interior pointers to fields,
// nested fields and array elements, pointer to slice element across a
// non-reallocating append, pointer-to-pointer, new(), per-iteration loop
// variable addresses, and nil dereference panics.
package main

type Leaf struct {
	V   int
	Arr [3]int
}

type Mid struct {
	L    Leaf
	Ls   [2]Leaf
	Name string
}

type Root struct {
	M  Mid
	PM *Mid
}

//go:noinline
func set(p *int, v int) { *p = v }

//go:noinline
func incAll(ps []*int) {
	for _, p := range ps {
		*p += 1
	}
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "panic:", r.(interface{ Error() string }).Error())
		}
	}()
	f()
	println(name, "ok")
}

func main() {
	var r Root
	pv := &r.M.L.V
	pa := &r.M.Ls[1].Arr[2]
	pl := &r.M.Ls[0]
	set(pv, 11)
	set(pa, 22)
	pl.V = 33
	pl.Arr[0] = 44
	println(r.M.L.V, r.M.Ls[1].Arr[2], r.M.Ls[0].V, r.M.Ls[0].Arr[0])

	// copying the struct does not carry interior pointer aliasing
	r2 := r
	set(pv, 111)
	println(r.M.L.V, r2.M.L.V)

	// whole-struct assign through pointer to nested field
	pm := &r.M
	*pm = Mid{Name: "fresh"}
	println(r.M.L.V, r.M.Ls[1].Arr[2], r.M.Name, *pv, *pa)
	set(pv, 5)
	println(r.M.L.V)

	// pointer to array and its elements
	arr := [4]int{1, 2, 3, 4}
	parr := &arr
	pe := &parr[2]
	*pe = 30
	parr[3] = 40
	println(arr[0], arr[1], arr[2], arr[3], len(parr))
	sl := parr[1:3]
	sl[0] = 20
	println(arr[1], cap(sl))

	// pointer to slice element across append without reallocation
	s := make([]int, 3, 10)
	p1 := &s[1]
	s = append(s, 7, 8)
	*p1 = 99
	println(s[1], len(s), cap(s))
	s = append(s, make([]int, 20)...) // reallocates
	*p1 = -1
	println(s[1], *p1)

	// pointer to pointer
	x := 1
	px := &x
	ppx := &px
	**ppx = 2
	y := 10
	*ppx = &y
	**ppx = 20
	println(x, y, *px, px == &y)

	// new()
	pn := new(Leaf)
	pn.Arr[1] = 9
	pn2 := new([2]Leaf)
	pn2[1].V = 8
	pi := new(int)
	*pi += 7
	println(pn.Arr[1], pn2[1].V, pn2[0].V, *pi)
	pp := new(*int)
	println(*pp == nil)
	*pp = pi
	**pp *= 3
	println(*pi)

	// Go 1.22 per-iteration loop variables
	var ptrs []*int
	for i := 0; i < 4; i++ {
		ptrs = append(ptrs, &i)
	}
	incAll(ptrs)
	for _, p := range ptrs {
		print(*p, " ")
	}
	println()
	var rptrs []*int
	for _, v := range []int{10, 20, 30} {
		rptrs = append(rptrs, &v)
	}
	println(*rptrs[0], *rptrs[1], *rptrs[2], rptrs[0] != rptrs[1])
	// loop var modified in body affects next iteration's copy
	var seen []int
	var lps []*int
	for i := 0; i < 10; i++ {
		lps = append(lps, &i)
		seen = append(seen, i)
		i += 2
	}
	for k := range seen {
		print(seen[k], ":", *lps[k], " ")
	}
	println()

	// pointer equality on fields
	type pair struct{ a, b int }
	pr := pair{}
	println(&pr.a == &pr.a, &arr[0] == &parr[0])
	var e1, e2 struct{}
	_ = &e1
	_ = &e2

	// pointer receivers through addressable array elements
	leaves := [3]Leaf{}
	for i := range leaves {
		leaves[i].bump(i + 1)
	}
	println(leaves[0].V, leaves[1].V, leaves[2].V, leaves[2].Arr[2])

	// nil derefs
	var np *Leaf
	var nr *Root
	try("field", func() { println(np.V) })
	try("arr", func() { println(np.Arr[1]) })
	try("store", func() { np.V = 1 })
	try("nested", func() { println(nr.PM.L.V) })
	try("nestedPM", func() { r.PM.Name = "x" })
	var na *[3]int
	try("arrlen", func() { println(len(na)) })
	try("arridx", func() { println(na[0]) })
	try("arrrange", func() {
		for i := range na {
			print(i)
		}
		println()
	})
	try("arrrange2", func() {
		for i, v := range na {
			print(i, v)
		}
	})
	var npi *int
	try("int", func() { *npi++ })
	try("method", func() { np.bump(1) })
	try("copy", func() { l := *np; println(l.V) })
}

func (l *Leaf) bump(k int) {
	l.V += k
	l.Arr[k-1] = k * 100
}
