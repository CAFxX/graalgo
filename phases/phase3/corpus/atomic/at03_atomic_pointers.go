// verifies: GG-0020
package main

import (
	"fmt"
	"sync/atomic"
)

// P3-M4 remediation (P0-1): pointer aliasing must survive the D5
// carrier. The cell behind a *atomic.Int64 IS the same object on both
// sides of a pointer copy or pointer-parameter call — cloning it would
// sever the alias (gc: stores through one pointer are visible through
// the other).

func setIt(p *atomic.Int64) {
	p.Store(9)
}

func main() {
	// Pointer parameter: store through the param is visible to the caller.
	p := new(atomic.Int64)
	setIt(p)
	fmt.Println(p.Load())

	// Pointer copy: q and r alias the same cell.
	q := new(atomic.Int64)
	r := q
	r.Store(5)
	fmt.Println(q.Load(), r.Load())

	// Interface-held pointer: the dynamic value is the pointer, so a
	// store through the asserted pointer is visible through the
	// interface's dynamic value.
	var w any = new(atomic.Int64)
	w.(*atomic.Int64).Store(7)
	fmt.Println(w.(*atomic.Int64).Load())

	// By-value still clones (regression anchor for the P0-1 gate): a
	// value copy must NOT see the original's later stores.
	var a atomic.Int64
	b := a
	a.Store(3)
	fmt.Println(a.Load(), b.Load())
}
