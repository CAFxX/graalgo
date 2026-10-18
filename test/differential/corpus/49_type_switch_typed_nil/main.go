package main

import "fmt"

// 49_type_switch_typed_nil: a typed nil must NOT match `case nil` in a type
// switch (gc 1.27.1: `case nil` matches only the nil interface). A typed nil
// is a non-nil interface whose dynamic type still has a clause: it matches
// that clause, and the bound value compares == nil. A true nil interface is
// the control. Deterministic by construction (no maps, no concurrency, no
// wall clock); verified byte-identical to gc 1.27.1 (stdout, stderr, exit
// code 0).

type myErr struct{ msg string }

func (e *myErr) Error() string { return e.msg }

func classify(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case *myErr:
		if x == nil {
			return "myerr-typednil"
		}
		return "myerr-value"
	case *int:
		if x == nil {
			return "intptr-typednil"
		}
		return "intptr-value"
	default:
		return "default"
	}
}

func main() {
	// Typed nils: non-nil interfaces with nil data.
	var e error = (*myErr)(nil)
	fmt.Println(classify(e))

	var tn any = (*int)(nil)
	fmt.Println(classify(tn))

	// True-nil control: only this one may hit `case nil`.
	var z any
	fmt.Println(classify(z))

	// Non-nil controls: dynamic-type clauses still match values.
	i := 7
	fmt.Println(classify(&i))
	fmt.Println(classify(&myErr{"boom"}))

	// Guardless form: typed nil must still hit its dynamic-type clause,
	// true nil must still hit `case nil`.
	switch tn.(type) {
	case nil:
		fmt.Println("guardless:nil")
	case *int:
		fmt.Println("guardless:intptr-typednil")
	default:
		fmt.Println("guardless:default")
	}
	switch z.(type) {
	case nil:
		fmt.Println("guardless:truenil")
	default:
		fmt.Println("guardless:unexpected")
	}
}
