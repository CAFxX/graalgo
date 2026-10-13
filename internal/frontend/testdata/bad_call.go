package main

func main() {
	type T struct {
		f func() int
	}
	t := T{}
	x := t.f()
	_ = x
}
