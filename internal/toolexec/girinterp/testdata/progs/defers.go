package main

// Defer ordering, argument evaluation time, named results, recover,
// go1.22 loop variables.

func order() (s string) {
	for i := 0; i < 3; i++ {
		defer func() { s += string(rune('a' + i)) }()
	}
	return "x"
}

func args() {
	v := 1
	defer println("deferred arg", v)
	v = 2
	defer func() { println("closure sees", v) }()
	v = 3
}

func named() (r int) {
	defer func() { r *= 2 }()
	return 21
}

func rec() (err string) {
	defer func() {
		if p := recover(); p != nil {
			err = "recovered: " + p.(string)
		}
	}()
	panic("boom")
}

func nested() {
	defer func() {
		r := recover()
		println("outer recover", r.(string))
	}()
	defer func() {
		defer func() {
			println("inner recover", recover().(string))
			panic("second")
		}()
		panic("first-in-defer")
	}()
	panic("original")
}

func loopvars() {
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	for _, v := range []int{10, 20, 30} {
		fs = append(fs, func() int { return v })
	}
	s := 0
	for _, f := range fs {
		s = s*100 + f()
	}
	println("loopvars", s)
	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
		i++
	}
	for _, p := range ps {
		println("ptr", *p)
	}
}

type T struct{ n int }

func (t T) show() { println("T.show", t.n) }

func methodDefer() {
	t := T{1}
	defer t.show()
	t.n = 2
	var i interface{ show() } = t
	t.n = 3
	defer i.show()
}

func main() {
	println(order())
	args()
	println(named())
	println(rec())
	nested()
	loopvars()
	methodDefer()
}
