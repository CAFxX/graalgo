package main

import (
	"fmt"
	"weak"
)

// SCOPE-LIMITED: the gc leg proves this program is valid, deterministic
// Go. The graalgo leg is skipped: weak.Pointer needs weak-reference
// runtime semantics (see 00_weak.go.scopelimited). Only the deterministic
// pins are asserted here — post-GC reclamation timing is deliberately NOT
// pinned (nondeterministic under any backend).
func main() {
	x := 42
	p1 := weak.Make(&x)
	p2 := weak.Make(&x)
	fmt.Println(p1 == p2) // same pointer -> equal pointers

	y := 42
	p3 := weak.Make(&y)
	fmt.Println(p1 == p3) // distinct objects -> unequal

	// While strongly reachable, Value is non-nil and correct.
	fmt.Println(p1.Value() == &x)
	fmt.Println(*p1.Value() == 42)

	// Nil pointer in -> nil pointer out.
	var np *int
	pn := weak.Make(np)
	fmt.Println(pn.Value() == nil)

	// Pointers to struct fields / array elements.
	type T struct{ a int }
	t := &T{a: 7}
	fp := weak.Make(&t.a)
	fmt.Println(fp.Value() == &t.a)
	fmt.Println(*fp.Value() == 7)

	arr := [3]string{"a", "b", "c"}
	ep := weak.Make(&arr[1])
	fmt.Println(*ep.Value() == "b")

	// Pointers from different offsets into one object are unequal.
	fmt.Println(weak.Make(&arr[0]) == weak.Make(&arr[1]))

	fmt.Println("ok")
}
