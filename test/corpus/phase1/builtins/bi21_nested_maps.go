package main

// Nested map %v printing at arbitrary depth.
//
// Regression test (M8 closeout): printMap dropped the inner map's key/elem
// kinds when recursing, so map[string]map[string]int crashed with
// "Long cannot be cast to GoInterface" (the inner int value was read at
// INTERFACE kind). The fix threads keyKindChain/elemKindChain per nesting
// level (parallel to the R2 P0-2 array fix), popped by nestedMapArg.
import "fmt"

func main() {
	// The exact crash: 2-level nesting.
	v18 := map[string]map[string]int{"hello world": {"héllo": 3}}
	fmt.Println(v18)

	// 3-level nesting, all verbs.
	m3 := map[string]map[string]map[string]int{"a": {"b": {"c": 42}}}
	fmt.Println(m3)
	fmt.Printf("%v\n", m3)
	fmt.Printf("%#v\n", m3)

	// Mixed inner value types.
	mm := map[string]map[string]string{"k": {"ik": "iv"}}
	fmt.Println(mm)

	// Deferred printing exercises the DeferNode/DeferredCall chain path.
	defer fmt.Println(m3)
	fmt.Println("done")
}

// Note: printing a nested map through an interface
// (var x any = v18; fmt.Println(x)) is a separate pre-existing gap:
// the interface print path drops composite kind info (the static type
// is any). Filed as follow-up; not covered here.
