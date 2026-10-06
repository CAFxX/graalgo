package main

import "fmt"

// P3-M3 R1 P0-2 regression: the builder's arg-adaptation boxing must use
// the factory's canonical descriptor. &T{...} is not otherwise interned
// in main's BuildCtx (only T is), so the old replicaDescriptor minted a
// non-canonical descriptor and the callee's x.(*T) / type switch missed.
// Both must now take the *T branch (gc parity).
type I interface {
	M(x any) string
	N(x any) string
}

type T struct{ v int }

func (t T) M(x any) string {
	if _, ok := x.(*T); ok {
		return "yes-*T"
	}
	return "no"
}

func (t T) N(x any) string {
	switch x.(type) {
	case *T:
		return "yes-*T"
	default:
		return "no"
	}
}

func main() {
	var i I = T{v: 1}
	fmt.Println(i.M(&T{v: 2}))
	fmt.Println(i.N(&T{v: 3}))
}
