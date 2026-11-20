// Targets exact runtime error message text for non-bounds failures,
// recovered: nil map write, integer divide by zero, nil pointer deref,
// channel misuse, negative shift, makeslice/makechan sizes, interface
// conversions, unhashable keys and comparing uncomparable values.
package main

import "runtime"

func try(name string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			println(name, "no panic")
			return
		}
		_, isRT := r.(runtime.Error)
		switch v := r.(type) {
		case interface{ Error() string }:
			println(name, isRT, v.Error())
		case string:
			println(name, "string", v)
		default:
			println(name, "other")
		}
	}()
	f()
}

type S struct{ a int }
type I interface{ M() }
type J interface{ N() }
type impl struct{}

func (impl) M() {}

type Named int

var sink int

func main() {
	var m map[string]int
	try("nilmap", func() { m["a"] = 1 })
	try("nilmap read", func() { sink = m["a"] })
	zero := 0
	try("div", func() { sink = 10 / zero })
	try("mod", func() { sink = 10 % zero })
	var p *S
	try("nilptr", func() { sink = p.a })
	var nc chan int
	try("close nil", func() { close(nc) })
	c := make(chan int, 1)
	close(c)
	try("close closed", func() { close(c) })
	try("send closed", func() { c <- 1 })
	sh := -3
	try("shift", func() { sink = 1 << sh })
	n := -1
	try("makeslice len", func() { _ = make([]int, n) })
	try("makeslice cap", func() { _ = make([]int, 2, 1+zero) })
	try("makechan", func() { _ = make(chan int, n) })
	var e any = "str"
	try("conv concrete", func() { sink = e.(int) })
	try("conv named", func() { sink = int(e.(Named)) })
	try("conv iface", func() { e.(I).M() })
	var ni any
	try("conv nil", func() { sink = ni.(int) })
	try("conv nil iface", func() { ni.(I).M() })
	var i I = impl{}
	try("conv missing", func() { i.(J).N() })
	try("conv ptr", func() { _ = i.(*impl) })
	mk := map[any]int{}
	try("unhashable slice", func() { mk[[]int{1}] = 1 })
	try("unhashable map", func() { sink = mk[map[int]int{}] })
	try("unhashable func", func() { mk[func() {}] = 1 })
	var x, y any = []int{1}, []int{1}
	try("cmp slice", func() { println(x == y) })
	type withSlice struct {
		s []int
	}
	var w1, w2 any = withSlice{}, withSlice{}
	try("cmp struct", func() { println(w1 == w2) })
	var a1, a2 any = [1]func(){}, [1]func(){}
	try("cmp array", func() { println(a1 == a2) })
	var ok1, ok2 any = 1, "1"
	try("cmp diff types", func() { println(ok1 == ok2) })
}
