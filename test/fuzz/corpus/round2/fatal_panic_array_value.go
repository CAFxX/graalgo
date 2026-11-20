// Fatal: unrecovered panic with a value of a named array type that was
// recovered, modified as a copy and re-panicked in a defer; before that,
// recovered warm-ups check that panicking with an array copies it. gc
// prints non-basic panic values as "(main.T) 0x..." with "[recovered]".
package main

type quad [4]int16

type grid [2]quad

func (q quad) sum() int { return int(q[0] + q[1] + q[2] + q[3]) }

func (q *quad) bump() { q[0]++ }

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func main() {
	q := quad{1, 2, 3, 4}
	got := catch(func() {
		defer q.bump()
		panic(q)
	})
	println("copied at panic", got.(quad)[0], q[0])

	g := grid{q, {5, 6, 7, 8}}
	got = catch(func() { panic(g) })
	g[1][0] = 50
	gg := got.(grid)
	println("grid copy", gg[1][0], gg[0].sum(), got == any(grid{{2, 2, 3, 4}, {5, 6, 7, 8}}))

	got = catch(func() { panic(&g[1]) })
	g[1][1] = 60
	println("pointer aliases", got.(*quad)[1], got.(*quad) == &g[1])

	got = catch(func() {
		defer func() { panic(recover().(quad).sum()) }()
		panic(quad{10, 20, 30, 40})
	})
	println("re-panicked sum", got.(int))

	defer func() {
		println("deferred runs, sum", q.sum())
	}()
	defer func() {
		r := recover()
		v := r.(quad)
		println("recovered first", v.sum())
		v[0] = 100
		q[3] = 40
		panic(v)
	}()
	var boxed any = q
	q[0] = -1
	panic(boxed)
}
