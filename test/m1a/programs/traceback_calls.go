// Traceback frames of a fatal panic (WP-TR-AUDIT-6): every Go frame from the
// panic site to main — methods through a pointer and through an interface, a
// closure inside a closure, a generic function — with gc's function names
// and the call sites' lines. Compared with frames by tbnorm.py.
// m1a:traceback
package main

type T struct{ n int }

func (t *T) Fail(depth int) {
	if depth == 0 {
		panic("boom")
	}
	t.Fail(depth - 1)
}

type Failer interface{ Go(int) }

func (t T) Go(n int) {
	p := &T{n: n}
	p.Fail(n)
}

func Apply[X any](x X, f func(X)) {
	f(x)
}

func main() {
	println("start")
	func() {
		func() {
			Apply(2, func(n int) {
				var f Failer = T{}
				f.Go(n)
			})
		}()
	}()
}
