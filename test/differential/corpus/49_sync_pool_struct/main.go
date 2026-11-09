// sync.Pool embedded in a struct (B23-01): zero values are independent
// pools, a copy before first use is independent, a copy after first use
// shares the pool, S{} literals get a fresh pool, [N]sync.Pool array
// slots are independent pools, and keyed literals / New via field
// assignment work. Deterministic by construction under GOMAXPROCS=1:
// each Get reads a pool's private slot, which holds exactly the one
// item Put into that pool (or is empty, so Get returns nil / calls New).
package main

import (
	"fmt"
	"sync"
)

type S struct {
	p sync.Pool
}

func main() {
	// 1. Two independent zero values.
	var a, b S
	a.p.Put("y")
	fmt.Println(b.p.Get() == nil)
	fmt.Println(a.p.Get())

	// 2. Copy before first use is independent.
	var c S
	d := c
	d.p.Put("w")
	fmt.Println(c.p.Get() == nil)

	// 3. Copy after first use shares the pool.
	var e S
	e.p.Put("z")
	f := e
	fmt.Println(f.p.Get())

	// 4. S{} literal gets a fresh pool; a second zero value is empty.
	g := S{}
	g.p.Put("lit")
	fmt.Println(g.p.Get())
	var h S
	fmt.Println(h.p.Get() == nil)

	// 5. [N]sync.Pool array slots are independent pools.
	var pools [2]sync.Pool
	pools[0].Put("zero")
	fmt.Println(pools[1].Get() == nil)
	fmt.Println(pools[0].Get())

	// 6. Keyed literal with New, and New via field assignment.
	i := S{p: sync.Pool{New: func() any { return "n" }}}
	fmt.Println(i.p.Get())
	var j S
	fmt.Println(j.p.New == nil)
	j.p.New = func() any { return "assigned" }
	fmt.Println(j.p.Get())
	fmt.Println(j.p.New().(string))
}
