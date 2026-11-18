package main

// A panic whose traceback crosses multi-line calls, a method, a closure
// and a deferred call: frames and lines must match gc's.

type T struct{ v []int }

func (t *T) at(i int) int {
	return t.v[ // line 9: index attributed to '['
	i]
}

func helper(a,
	b int) int {
	t := &T{v: []int{1, 2}}
	return t.
		at( // gc attributes the call to this line, not the receiver's
			a + b)
}

func main() {
	defer func() {
		println("deferred runs")
	}()
	f := func() int {
		return helper(
			2,
			3)
	}
	println(f())
}
