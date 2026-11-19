// Targets slice aliasing: append sharing backing arrays, 3-index slices
// limiting cap, copy with overlap in both directions, copy string->[]byte,
// nil vs empty slices, slicing beyond len within cap, and slice bound panics.
package main

import "strconv"

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

func dump(name string, s []int) {
	print(name, " len=", len(s), " cap=", cap(s), " [")
	for i, v := range s {
		if i > 0 {
			print(" ")
		}
		print(v)
	}
	println("]")
}

//go:noinline
func reslice(s []int, i, j int) []int { return s[i:j] }

//go:noinline
func reslice3(s []int, i, j, k int) []int { return s[i:j:k] }

func main() {
	base := make([]int, 5, 10)
	for i := range base {
		base[i] = i * 10
	}
	a := append(base, 100)
	b := append(base, 200) // overwrites a[5]
	dump("a", a)
	dump("b", b)
	a[0] = -1
	dump("base", base)
	// 3-index slice forces copy on append
	c := base[1:3:3]
	d := append(c, 777)
	d[0] = 888
	dump("c", c)
	dump("d", d)
	dump("base2", base)
	// extending slice within cap
	e := base[:2]
	f := e[:cap(e)]
	dump("f", f)
	g := base[2:4]
	dump("g-ext", g[:cap(g)])
	// copy with overlap forward and backward
	x := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	n := copy(x[2:], x)
	println("copyfwd", n)
	dump("x", x)
	y := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	n = copy(y, y[3:])
	println("copyback", n)
	dump("y", y)
	z := []int{0, 1, 2, 3, 4}
	n = copy(z[1:3], z[2:])
	println("copypartial", n)
	dump("z", z)
	// copy with empty / nil
	var nilS []int
	println("copynil", copy(nilS, x), copy(x, nilS))
	// copy string into []byte
	bs := make([]byte, 5)
	n = copy(bs, "héllo wörld")
	println("copystr", n, strconv.Quote(string(bs)))
	bs2 := make([]byte, 20)
	n = copy(bs2[3:], "\xff€")
	println("copystr2", n, strconv.Quote(string(bs2[:8])))
	// nil vs empty
	var ns []string
	es := []string{}
	ms := make([]string, 0)
	println("nil?", ns == nil, es == nil, ms == nil, len(ns), cap(ns), len(es))
	ns2 := ns[:0]
	println("nilreslice", ns2 == nil)
	es2 := es[0:0]
	println("emptyreslice", es2 == nil)
	ns = append(ns, "a")
	// cap is not printed: gc gives a non-escaping append a 32-byte stack
	// buffer (cap 2 here), an escape-analysis artifact the spec leaves open.
	println("appendnil", ns == nil, len(ns), cap(ns) >= 1, ns[0])
	var nb []byte
	nb = append(nb, ""...)
	println("appendemptystr", nb == nil)
	nb = append(nb, "q"...)
	println("appendstr", nb == nil, len(nb))
	// slice of array; modifying array
	arr := [6]int{1, 2, 3, 4, 5, 6}
	sl := arr[1:4]
	arr[2] = 99
	dump("sl", sl)
	sl = append(sl, 42) // writes arr[4]
	println("arr4", arr[4])
	sl = append(sl, 43, 44) // exceeds cap -> new array
	sl[0] = -7
	println("arr1", arr[1], len(sl), cap(sl))
	// panics
	protect("hi>cap", func() { dump("p", reslice(base, 0, 11)) })
	protect("lo>hi", func() { dump("p", reslice(base, 3, 2)) })
	protect("3idx k>cap", func() { dump("p", reslice3(base, 0, 1, 11)) })
	protect("3idx j>k", func() { dump("p", reslice3(base, 0, 5, 4)) })
	protect("3idx ok", func() { dump("p", reslice3(base, 2, 3, 7)) })
	protect("neg", func() { dump("p", reslice(base, -1, 2)) })
	protect("index", func() {
		s := reslice(base, 0, 2)
		i := 5
		println(s[i])
	})
	protect("nilindex", func() {
		var s []int
		println(s[0])
	})
	// in-place filter that aliases
	vals := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	keep := vals[:0]
	for _, v := range vals {
		if v%3 != 0 {
			keep = append(keep, v)
		}
	}
	dump("keep", keep)
	dump("vals", vals)
	// reverse in place via two aliases
	r := []int{1, 2, 3, 4, 5, 6, 7}
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	dump("rev", r)
	// self-append
	sa := []int{1, 2, 3}
	sa = append(sa, sa...)
	sa = append(sa[:2], sa[3:]...)
	dump("sa", sa)
	// append to subslice clobbering later elements
	q := []int{1, 2, 3, 4, 5}
	q2 := append(q[:1], q[3:]...)
	dump("q", q)
	dump("q2", q2)
	// slice of slice of strings sharing
	ss := []string{"a", "b", "c", "d"}
	ss1 := ss[1:3]
	ss1[0] = "B"
	ss1 = append(ss1, "X")
	println("ss", ss[0], ss[1], ss[2], ss[3], len(ss1))
	// bytes alias
	buf := []byte("abcdef")
	p1 := buf[0:3]
	p2 := buf[2:6]
	p1[2] = 'Z'
	println("bytes", string(p1), string(p2))
	copy(p2, p1)
	println("bytes2", string(buf))
}
