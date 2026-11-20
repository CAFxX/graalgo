// Targets closures: capture by reference, per-iteration loop variables for
// three-clause and range loops, closures returned from loops, recursive
// closures via var f func, named results modified by deferred closures, and
// closures capturing struct/array values.
package main

import "strconv"

type acc struct {
	n   int
	arr [3]int
}

func counter() (func() int, func(int)) {
	c := 0
	return func() int { c++; return c }, func(v int) { c = v }
}

func namedResult(x int) (r int, s string) {
	defer func() {
		r *= 2
		s += "-deferred"
	}()
	defer func() {
		if x < 0 {
			r = recoverInt()
		}
	}()
	r = x + 1
	s = "v" + strconv.Itoa(r)
	return r + 10, s
}

func recoverInt() int { return -100 }

func panicky(x int) (res int) {
	defer func() {
		if e := recover(); e != nil {
			res = x * 1000
		}
	}()
	res = x
	if x%2 == 1 {
		var a []int
		_ = a[x]
	}
	return res + 1
}

func makeAdders() []func(int) int {
	var fs []func(int) int
	for i := 0; i < 3; i++ {
		fs = append(fs, func(x int) int { return x + i })
	}
	for _, k := range []int{100, 200} {
		fs = append(fs, func(x int) int { return x + k })
	}
	return fs
}

func deferOrder() (out string) {
	for i := 0; i < 4; i++ {
		defer func() { out += strconv.Itoa(i) }()
	}
	for i := range 3 {
		defer func(j int) { out += "<" + strconv.Itoa(j) + strconv.Itoa(i) + ">" }(i * 10)
	}
	return "start:"
}

func main() {
	inc, set := counter()
	println(inc(), inc())
	set(10)
	println(inc())
	inc2, _ := counter()
	println(inc2(), inc())

	fs := makeAdders()
	for _, f := range fs {
		print(f(1), " ")
	}
	println()

	// capture by reference vs by value of loop var (each iteration distinct)
	x := 5
	g := func() int { return x * 2 }
	x = 7
	println(g())
	var gs []func() int
	for i := 0; i < 3; i++ {
		gs = append(gs, func() int { i *= 10; return i })
	}
	println(gs[0](), gs[0](), gs[1](), gs[2]())

	// closure modifying loop var in three-clause loop affects that iteration
	var trace []int
	for i := 0; i < 10; i++ {
		bump := func() { i += 3 }
		bump()
		trace = append(trace, i)
	}
	for _, t := range trace {
		print(t, ",")
	}
	println()

	// recursive closure
	var fib func(int) int
	fib = func(n int) int {
		if n < 2 {
			return n
		}
		return fib(n-1) + fib(n-2)
	}
	println(fib(20))
	saved := fib
	fib = func(n int) int { return -1 }
	println(saved(5)) // inner calls now use new fib

	// mutual recursion via vars
	var even, odd func(int) bool
	even = func(n int) bool { return n == 0 || odd(n-1) }
	odd = func(n int) bool { return n != 0 && even(n-1) }
	println(even(10), odd(7), even(7))

	// named results and defer
	r, s := namedResult(3)
	println(r, s)
	r, s = namedResult(-1)
	println(r, s)
	println(panicky(4), panicky(5))
	println(deferOrder())

	// closures capturing structs and arrays by reference
	a := acc{n: 1}
	addN := func(k int) { a.n += k; a.arr[k%3] += k }
	addN(1)
	addN(2)
	addN(4)
	cp := a
	addN(5)
	println(a.n, a.arr[0], a.arr[1], a.arr[2], cp.n, cp.arr[2])

	arr := [3]int{1, 2, 3}
	sumArr := func() int { return arr[0] + arr[1] + arr[2] }
	arr[1] = 20
	arr = [3]int{100, 100, 100}
	println(sumArr())

	// closure parameter shadows capture
	y := 1
	h := func(y int) int { return y * 3 }
	k := func() int { y := y + 1; return y }
	println(h(5), k(), y)

	// immediately invoked + nested closure chains
	mk := func(base int) func(int) func() int {
		return func(step int) func() int {
			cur := base
			return func() int { cur += step; base++; return cur*100 + base }
		}
	}
	m1 := mk(1)
	c1 := m1(2)
	c2 := m1(5)
	println(c1(), c1(), c2(), c1(), c2())

	// closures in range-over-int capturing per-iteration vars
	var hs []func() string
	for i := range 3 {
		j := i * i
		hs = append(hs, func() string { return strconv.Itoa(i) + "/" + strconv.Itoa(j) })
	}
	for _, f := range hs {
		print(f(), " ")
	}
	println()

	// goto-built loop shares a single variable
	var gl []func() int
	n := 0
loop:
	if n < 3 {
		gl = append(gl, func() int { return n })
		n++
		goto loop
	}
	println(gl[0](), gl[1](), gl[2]())
}
