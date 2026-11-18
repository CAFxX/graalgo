package main

// Recover and repanic of the same value, and a deferred call at the
// closing brace that panics.

type myErr struct{ code int }

func (e myErr) Error() string { return "code " + string(rune('0'+e.code)) }

func inner() {
	defer func() {
		r := recover()
		println("recovered", r.(error).Error())
		panic(r)
	}()
	panic(myErr{4})
}

func tail() {
	defer func() {
		panic("from defer")
	}()
	println("tail body")
}

func main() {
	defer func() {
		r := recover()
		println("main recovered", r.(error).Error())
		tail()
	}()
	inner()
}
