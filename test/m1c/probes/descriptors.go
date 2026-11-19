// Probe "descriptors" (test/m1c/run.sh): the descriptor images of W9a
// (toolexec-design §8.1). errors' package initializer reads the type
// descriptor of `error` through internal/reflectlite, errors.Is/As walk
// descriptors, and sync.Map's HashTrieMap takes the key hasher from its map
// type's descriptor. Programs marked `//m1c:requires descriptors` run once
// this matches gc.
package main

import (
	"errors"
	"sync"
)

type myErr struct{ s string }

func (e *myErr) Error() string { return e.s }

func main() {
	base := errors.New("base")
	var target *myErr
	println(errors.Is(base, base), errors.As(&myErr{"x"}, &target), target.s)
	var m sync.Map
	m.Store("k", 1)
	v, ok := m.Load("k")
	println(v.(int), ok)
}
