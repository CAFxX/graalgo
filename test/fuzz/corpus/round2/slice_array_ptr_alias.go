// Slice <-> array conversions and aliasing: (*[N]T)(s) shares storage,
// [N]T(s) copies; pointers to elements survive reslicing, 3-index slicing
// forces append to reallocate; length-mismatch conversion panics.
package main

type pair struct {
	a int32
	b [2]uint8
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func main() {
	s := []int{0, 1, 2, 3, 4, 5, 6, 7}
	ap := (*[4]int)(s[2:])
	ap[0] = 20
	println("alias", s[2], len(ap))
	av := [4]int(s[4:])
	av[0] = 40
	println("copy", s[4], av[0])
	a0 := [0]int(s[:0])
	println("zero len", len(a0))
	var nilS []int
	println("nil to *[0]", (*[0]int)(nilS) == nil, len([0]int(nilS)))
	println("empty to *[0]", (*[0]int)(s[:0]) != nil)

	try("short to array", func() { _ = [3]int(s[6:]) })
	try("short to ptr", func() { _ = (*[9]int)(s) })
	try("nil to *[1]", func() { _ = (*[1]int)(nilS) })

	e := &s[1]
	s = s[1:3]
	*e = 11
	println("elem ptr after reslice", s[0], sum(s))
	s = s[:cap(s)]
	println("extend to cap", len(s), s[6])

	base := make([]pair, 3, 10)
	pp := &base[1]
	pb := &base[1].b[1]
	grown := append(base, pair{a: 9})
	pp.a = 5
	*pb = 6
	println("append within cap aliases", grown[1].a, grown[1].b[1])
	limited := base[:2:2]
	grown2 := append(limited, pair{a: 8})
	grown2[0].a = 77
	println("3-index forces copy", base[0].a, grown2[0].a, base[2].a, grown2[2].a)

	arr := [5]pair{}
	sl := arr[1:4]
	sl[0].b[0] = 1
	arrp := (*[3]pair)(sl)
	arrp[2].a = 3
	println("array via slice", arr[1].b[0], arr[3].a)
	cp := [3]pair(sl)
	cp[0].a = 100
	println("array copy", arr[1].a, cp[0].a, cp == *arrp)

	m := [][2]int{{1, 2}, {3, 4}}
	p2 := &m[1]
	m = append(m, [2]int{5, 6})
	p2[0] = 30
	println("old backing", m[1][0], p2[0])

	bs := []byte("hello, world")
	h := (*[5]byte)(bs)
	h[0] = 'H'
	w := [5]byte(bs[7:])
	println("bytes", string(bs), string(h[:]), string(w[:]))

	n := copy(bs[2:], bs)
	println("overlap copy", n, string(bs))
	ints := []int{1, 2, 3, 4, 5}
	copy(ints, ints[1:])
	println("copy left", ints[0], ints[3], ints[4])

	var grid [3][3]int
	rowS := grid[1][:]
	rowS[2] = 9
	colP := &grid[2]
	rowS = colP[1:]
	rowS[0] = 8
	println("grid", grid[1][2], grid[2][1])
	ss := grid[:]
	ss[0][0] = 1
	println("slice of array of arrays", grid[0][0], len(ss))

	str := "abcdef"
	bsl := []byte(str[1:4])
	bsl[0] = 'X'
	println("string copy", str, string(bsl))
	ar := [3]byte(bsl)
	ar[1] = 'Y'
	println("array of bytes", string(bsl), string(ar[:]))

	vals := make([]int, 0, 4)
	vals = append(vals, 1, 2)
	a := vals[:1]
	b := append(a, 100)
	println("shared append", vals[1], b[1])
	clr := []pair{{a: 1}, {a: 2}}
	pc := &clr[0]
	clear(clr)
	println("clear through ptr", pc.a, clr[1].a)
}
