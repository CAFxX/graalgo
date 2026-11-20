// Method values bound through embedded pointer and value fields: the
// receiver (pointer or copied value) is fixed when the method value is
// evaluated, not when it is called; promoted methods via interfaces;
// method expressions on arrays/structs; nil embedded pointer panics.
package main

type inner struct {
	n   int
	arr [2]int
}

func (i inner) Val() int  { return i.n*100 + i.arr[1] }
func (i *inner) Ptr() int { return i.n*100 + i.arr[1] }
func (i *inner) Inc()     { i.n++ }

type outerP struct {
	*inner
	tag string
}

type outerV struct {
	inner
	tag string
}

type tri [3]int

func (t tri) Sum() int   { return t[0] + t[1] + t[2] }
func (t *tri) Set(v int) { t[0] = v }
func (t tri) With(v int) tri {
	t[1] = v
	return t
}

type valuer interface{ Val() int }
type ptrer interface{ Ptr() int }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func main() {
	a := &inner{n: 1, arr: [2]int{0, 5}}
	b := &inner{n: 2, arr: [2]int{0, 6}}
	o := outerP{inner: a, tag: "o"}
	val := o.Val
	ptr := o.Ptr
	inc := o.Inc
	o.inner = b
	a.n = 10
	println("after swap", val(), ptr(), o.Val(), o.Ptr())
	inc()
	println("inc bound to a", a.n, b.n)

	ov := outerV{inner: inner{n: 3}}
	v2 := ov.Val
	p2 := ov.Ptr
	ov.n = 4
	ov.arr[1] = 9
	println("embedded value", v2(), p2(), ov.Val())
	ovs := []outerV{ov, ov}
	pinc := ovs[1].Inc
	ovs = append(ovs, ov)
	pinc()
	println("bound to old backing", ovs[1].n, ovs[0].n)

	var vi valuer = o
	var pi ptrer = o
	o.inner = a
	b.n = 20
	println("iface copies outer", vi.Val(), pi.Ptr())
	var vi2 valuer = ov
	ov.n = 50
	println("iface copies value embed", vi2.Val(), ov.Val())
	var pi2 ptrer = &ov
	ov.n = 60
	println("iface via ptr", pi2.Ptr())
	mv := vi.Val
	vi = ov
	println("method value from iface", mv(), vi.Val())

	t := tri{1, 2, 3}
	sum := t.Sum
	set := t.Set
	t[2] = 30
	set(7)
	println("array methods", sum(), t.Sum(), t[0])
	w := t.With(100)
	println("value method copy", t[1], w[1])
	fs := tri.Sum
	fp := (*tri).Set
	fp(&w, -1)
	println("method exprs", fs(w), fs(t))
	pt := &t
	psum := pt.Sum
	pt[0] = 0
	println("ptr auto-deref copies", psum(), pt.Sum())

	funcs := make([]func() int, 0, 3)
	arrs := []tri{{1}, {2}, {3}}
	for _, x := range arrs {
		funcs = append(funcs, x.Sum)
	}
	for i := range arrs {
		arrs[i][0] = 0
	}
	for _, f := range funcs {
		print(f(), " ")
	}
	println()
	pfuncs := []func(int){}
	for i := range arrs {
		pfuncs = append(pfuncs, arrs[i].Set)
	}
	for i, f := range pfuncs {
		f(i + 10)
	}
	println("ptr method values", arrs[0][0], arrs[2][0])

	var nilo outerP
	try("nil embedded value method value", func() {
		f := nilo.Val
		println("not reached", f())
	})
	f := nilo.Ptr
	try("nil embedded ptr method call", func() { println(f()) })
	var np *inner
	g := np.Inc
	try("nil receiver call", func() { g() })
	var ni valuer
	try("nil iface method value", func() { _ = ni.Val })
}
