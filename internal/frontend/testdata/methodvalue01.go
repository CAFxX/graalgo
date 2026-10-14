package main

// Regression: Gate-1 sort divergence — calling a function value selected
// out of a struct field through an index (h.less[i](a, b), the sort
// multiSorter pattern from example_multi_test.go) failed with
// "selector call ms.less is not supported in frontend/v1 scope (WS D)".
// Also pins true method values (add := c.add; add(d)).

type lessFunc func(a, b int) bool

type holder struct {
	less []lessFunc
}

func (h *holder) pick(i int, a, b int) bool {
	return h.less[i](a, b)
}

type counter struct {
	n int
}

func (c *counter) add(d int) int {
	c.n += d
	return c.n
}

func main() {
	h := &holder{less: []lessFunc{
		func(a, b int) bool { return a < b },
		func(a, b int) bool { return a > b },
	}}
	t := h.pick(0, 3, 4)
	f := h.pick(1, 3, 4)

	c := &counter{}
	add := c.add // method value: receiver bound
	r1 := add(5)
	r2 := add(7)

	println(t, f, r1, r2, c.n)
}
