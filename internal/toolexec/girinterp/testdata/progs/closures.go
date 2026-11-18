package main

// Closures capturing by reference, nested captures, method values and
// expressions, embedded promotion (incl. pointer and interface embedding,
// unnamed structs with promoted methods).

type Counter struct{ n int }

func (c *Counter) Inc() int { c.n++; return c.n }
func (c Counter) Get() int  { return c.n }

type Named struct {
	Counter
	name string
}

type PNamed struct {
	*Counter
}

type Getter interface{ Get() int }

type HasGetter struct {
	Getter
	extra int
}

func makeAdder() (func(int) int, func() int) {
	sum := 0
	add := func(d int) int {
		sum += d
		return sum
	}
	get := func() int {
		inner := func() int { return sum * 10 }
		return inner()
	}
	return add, get
}

func main() {
	add, get := makeAdder()
	add(3)
	add(4)
	println(get())

	x := 1
	f := func() { x *= 5 }
	f()
	f()
	println(x)

	var c Counter
	inc := c.Inc
	inc()
	inc()
	getv := c.Get
	c.n = 100
	println(c.n, getv())

	me := (*Counter).Inc
	gv := Counter.Get
	println(me(&c), gv(c))

	nm := Named{name: "n"}
	nm.Inc()
	nm.Counter.Inc()
	println(nm.Get(), nm.n)
	promoted := Named.Get
	ppromo := (*Named).Inc
	println(promoted(nm), ppromo(&nm))

	pn := PNamed{&Counter{n: 5}}
	pn.Inc()
	pv := PNamed.Inc
	println(pv(pn), pn.Get(), pn.n)

	var g Getter = nm
	println(g.Get())
	hg := HasGetter{Getter: Counter{n: 9}}
	println(hg.Get(), HasGetter.Get(hg))
	g = hg
	println(g.Get())

	// unnamed struct types with promoted methods, through interfaces
	var g2 Getter = struct {
		x int
		Counter
	}{1, Counter{n: 11}}
	println(g2.Get())
	var g3 interface{ Inc() int } = &struct{ *Counter }{&Counter{n: 20}}
	println(g3.Inc(), g3.Inc())
	_, isGetter := g3.(Getter)
	println(isGetter)

	funcs := map[string]func(int) int{"dbl": func(v int) int { return v * 2 }}
	println(funcs["dbl"](21))
	var nf func()
	println(nf == nil)
}
