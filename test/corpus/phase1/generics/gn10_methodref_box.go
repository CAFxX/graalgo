package main

type Box[T any] struct {
	v T
}

// Dump takes an interface-typed parameter. The methodref call path must
// box the concrete argument with its exact static-type descriptor at the
// call site; otherwise the type switch below silently takes the default
// branch (M6 P1-1: GoFuncRootNode.wrapInterface inferred a bogus
// descriptor from the Java carrier type).
func (b Box[T]) Dump(i any) T {
	switch x := i.(type) {
	case Box[T]:
		return x.v
	default:
		return b.v
	}
}

func main() {
	b := Box[int]{1}
	a := b.Dump(Box[int]{2})
	c := b.Dump(42)
	a = a
	c = c
}

// verifies: GG-0014
// observe: a c
