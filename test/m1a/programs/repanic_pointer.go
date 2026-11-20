package main

// A recovered panic re-raised with the same pointer, re-boxed through a type
// assertion (panic(r.(*E))), is "[recovered, repanicked]" under gc: the two
// interface words are equal. Found by test/fuzz (BUG-0043).

type E struct{ code int }

func (e *E) Error() string { return "code error" }

func main() {
	defer println("main defer")
	defer func() {
		r := recover()
		e := r.(*E)
		e.code++
		panic(e)
	}()
	panic(&E{1})
}
