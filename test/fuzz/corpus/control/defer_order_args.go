// Targets defer ordering (LIFO), defers inside loops (heap-allocated vs
// open-coded), defer arguments and method receivers evaluated at defer time,
// per-iteration loop variables captured by deferred closures, and defers in
// conditionally executed branches.
package main

import "strconv"

type acc struct{ log []string }

func (a *acc) add(s string) { a.log = append(a.log, s) }

type val struct{ n int }

func (v val) show(a *acc, tag string) { a.add(tag + "=" + strconv.Itoa(v.n)) }

func simple(a *acc) {
	defer a.add("first")
	defer a.add("second")
	defer a.add("third")
	a.add("body")
}

func loopDefers(a *acc, n int) {
	for i := 0; i < n; i++ {
		defer a.add("arg" + strconv.Itoa(i))
		defer func() { a.add("clo" + strconv.Itoa(i)) }()
	}
	a.add("loop-body")
}

func argsAtDeferTime(a *acc) {
	x := 1
	defer a.add("x-arg=" + strconv.Itoa(x))
	defer func() { a.add("x-clo=" + strconv.Itoa(x)) }()
	x = 2
	v := val{10}
	defer v.show(a, "recv")
	p := &v
	defer p.show(a, "precv")
	v.n = 20
	x = 3
}

func condDefers(a *acc, mask int) {
	if mask&1 != 0 {
		defer a.add("c1")
	}
	if mask&2 != 0 {
		defer a.add("c2")
	}
	switch mask {
	case 3:
		defer a.add("c3")
	default:
		defer a.add("cd")
	}
	a.add("cbody" + strconv.Itoa(mask))
}

func deferFuncValue(a *acc) {
	f := func(s string) { a.add("f1:" + s) }
	defer f("a")
	f = func(s string) { a.add("f2:" + s) }
	defer f("b")
}

func nestedDeferInDefer(a *acc) {
	defer func() {
		defer a.add("inner2")
		defer a.add("inner1")
		a.add("outer-defer")
	}()
	a.add("nbody")
}

func counter(a *acc, n int) (r int) {
	for i := range n {
		defer func() { r += i }()
	}
	return 100
}

func main() {
	a := &acc{}
	simple(a)
	loopDefers(a, 3)
	argsAtDeferTime(a)
	for m := 0; m < 4; m++ {
		condDefers(a, m)
	}
	deferFuncValue(a)
	nestedDeferInDefer(a)
	a.add("counter=" + strconv.Itoa(counter(a, 5)))
	a.add("counter0=" + strconv.Itoa(counter(a, 0)))
	for i, s := range a.log {
		println(i, s)
	}
}
