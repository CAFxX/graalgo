// Targets the fatal printout when a deferred func recovers panic A and then
// panics with a different value B: gc prints "panic: A [recovered]" then
// "\tpanic: B" with a nested custom error type; exit 2.
package main

type wrapErr struct {
	op    string
	inner string
}

func (w *wrapErr) Error() string { return w.op + ": " + w.inner }

func step(name string) {
	defer func() {
		r := recover()
		println("recovered in", name, r.(string))
		panic(&wrapErr{op: name, inner: r.(string)})
	}()
	panic("disk full")
}

func main() {
	total := 0
	for i := 1; i <= 4; i++ {
		defer func() { total += i }()
	}
	println("deferred loop registered")
	step("write")
}
