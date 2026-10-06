package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	// Zero value is usable (no explicit init).
	var z atomic.Int64
	fmt.Println(z.CompareAndSwap(0, 99), z.Load())

	// new() form.
	p := new(atomic.Bool)
	p.Store(true)
	fmt.Println(p.Load(), p.Swap(false))

	// Cells are values in Go: assignment copies the integer (noCopy
	// semantics — copying an atomic cell is a vet error, but defined).
	var a atomic.Int64
	b := a
	b.Store(7)
	fmt.Println(a.Load(), b.Load())

	// Function argument: by-value copy.
	fmt.Println(inc(a))
	fmt.Println(a.Load())
}

func inc(x atomic.Int64) int64 {
	x.Add(10)
	return x.Load()
}
