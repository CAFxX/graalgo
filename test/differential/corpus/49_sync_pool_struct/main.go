// sync.Pool embedded in a struct (B23-01): zero values are independent
// pools, fresh values from a helper are independent, pointer aliasing
// shares the pool, S{} literals get a fresh pool, [N]sync.Pool array
// slots are independent pools, and keyed literals / New via field
// assignment work. Deterministic by construction under GOMAXPROCS=1:
// each Get reads a pool's private slot, which holds exactly the one
// item Put into that pool (or is empty, so Get returns nil / calls New).
//
// Struct values holding a sync.Pool are never copied here. sync.Pool
// must not be copied after first use, and `go vet` copylocks — run by
// the smoke net — rejects every copy of a lock-containing value:
// plain assignment, generic helpers, append, channel send,
// reflect.ValueOf, and field-wise literals (verified 2026-11-09
// against go1.27.1; the copy builtin is the lone unflagged mechanism
// and using it would still copy the lock while dodging vet, so it is
// not used). Independence of a second value is therefore probed with a
// fresh value from a helper (each evaluation owns its own pool via the
// zero template's deepCopy), and sharing with pointer aliasing, the
// defined mechanism. The struct-assignment copy path itself is covered
// at the mechanism level by StructPoolZeroTemplateTest
// (GoStruct.deepCopy), not by this program.
package main

import (
	"fmt"
	"sync"
)

type S struct {
	p sync.Pool
}

// freshS returns a fresh zero value; each evaluation owns its own pool.
func freshS() S { return S{} }

func main() {
	// 1. Two independent zero values.
	var a, b S
	a.p.Put("y")
	fmt.Println(b.p.Get() == nil)
	fmt.Println(a.p.Get())

	// 2. A fresh value from a helper is independent.
	var c S
	d := freshS()
	d.p.Put("w")
	fmt.Println(c.p.Get() == nil)

	// 3. Pointer aliasing shares the pool.
	var e S
	e.p.Put("z")
	f := &e
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
