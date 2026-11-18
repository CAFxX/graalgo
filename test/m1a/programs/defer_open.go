package main

// Open-coded defers: every defer statement runs at most once.

type T struct{ n int }

func (t *T) add(d int) { t.n += d }

func order() (s string) {
	defer func() { s += "a" }()
	if len(s) == 0 {
		defer func() { s += "b" }()
	}
	defer func(x string) { s += x }("c")
	return "r"
}

func conditional(run bool) (n int) {
	if run {
		defer func() { n += 10 }()
	}
	defer func() { n++ }()
	return 1
}

func recovers() (r any) {
	defer func() { r = recover() }()
	var m map[string]int
	m["x"] = 1
	return nil
}

func nested() (out string) {
	defer func() {
		defer func() {
			if r := recover(); r != nil {
				out += " inner:" + r.(string)
			}
		}()
		if r := recover(); r != nil {
			out += "outer:" + r.(string)
			panic("second")
		}
	}()
	panic("first")
}

func helper() any { return recover() }

func notDirect() (r any) {
	defer func() {
		r = helper() // not called directly by the deferred function: nil
		recover()
	}()
	panic("x")
}

func methodDefer() int {
	t := &T{}
	defer t.add(1)
	defer t.add(2)
	t.add(40)
	return t.n
}

func argsEarly() (n int) {
	x := 1
	defer func(v int) { n += v }(x)
	x = 100
	return x
}

func deferBuiltins() (r any) {
	defer func() { r = recover() }()
	defer println("deferred println", 1, 2)
	defer panic("from deferred panic")
	return nil
}

func deferRecoverDirect() (r any) {
	defer func() { r = recover() }()
	defer recover() // does not recover
	panic("still panicking")
}

func main() {
	println(order())
	println(conditional(true), conditional(false))
	e := recovers().(error)
	println(e.Error())
	println(nested())
	println(notDirect() == nil)
	println(methodDefer())
	println(argsEarly())
	println(deferBuiltins().(string))
	println(deferRecoverDirect().(string))
}
