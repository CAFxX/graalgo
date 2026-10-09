package main

import (
	"fmt"
	"unique"
)

func main() {
	// Pointer values canonicalize by pointer identity, NOT pointee value.
	// (WS D: & only on composite literals, so the pointers come from
	// &struct{...}{...} evaluated once and reused.)
	p := &struct{ v int }{1}
	q := &struct{ v int }{1}
	fmt.Println(unique.Make(p) == unique.Make(p)) // same pointer
	fmt.Println(unique.Make(p) == unique.Make(q)) // distinct pointers

	// any-typed values: dynamic type participates in identity.
	var x any = 42
	var y any = 42
	var z any = "42"
	fmt.Println(unique.Make(x) == unique.Make(y))
	fmt.Println(unique.Make(x).Value())
	// x (any holding int) vs z (any holding string): different dynamics.
	// (Not directly comparable across instantiations; pin separately.)
	fmt.Println(unique.Make(z).Value())

	// Zero values canonicalize like any other value.
	fmt.Println(unique.Make(0) == unique.Make(0))
	type empty struct{}
	fmt.Println(unique.Make(empty{}) == unique.Make(empty{}))

	// 5000 distinct values -> 5000 distinct handles.
	seen := map[unique.Handle[int]]bool{}
	for i := 0; i < 5000; i++ {
		seen[unique.Make(i)] = true
	}
	fmt.Println(len(seen))

	// Struct with unexported fields.
	type priv struct {
		a int
		b string
	}
	q1 := unique.Make(priv{1, "x"})
	fmt.Println(q1 == unique.Make(priv{1, "x"}))
	fmt.Println(q1 == unique.Make(priv{1, "y"}))
	fmt.Println(q1.Value())

	// Nested arrays/structs.
	type nest struct {
		m [2][2]int
		s string
	}
	n1 := unique.Make(nest{[2][2]int{{1, 2}, {3, 4}}, "n"})
	fmt.Println(n1 == unique.Make(nest{[2][2]int{{1, 2}, {3, 4}}, "n"}))
	fmt.Println(n1.Value().s)
}
