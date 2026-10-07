// 12_visiblefields_embedded_ptr: VisibleFields promotes fields through
// embedded *T (P1-2). gc dereferences one pointer level before the
// struct-kind check.
package main

import (
	"fmt"
	"os"
	"reflect"
)

type T struct {
	A int
	B string
}
type U struct {
	C float64
}
type S struct {
	*T
	U
	D bool
}

func main() {
	fields := reflect.VisibleFields(reflect.TypeOf(S{}))
	// gc 1.27.1 returns: T, A, B (from *T), U, C (from U), D — 6 fields.
	// The embedded fields themselves (T, U) are included.
	names := []string{}
	for _, f := range fields {
		names = append(names, f.Name)
	}
	fmt.Println(names)
	if len(names) != 6 {
		fmt.Println("FAIL: expected 6 visible fields, got", len(names))
		os.Exit(1)
	}
	// Check the specific names.
	want := map[string]bool{"T": true, "A": true, "B": true, "U": true, "C": true, "D": true}
	for _, n := range names {
		if !want[n] {
			fmt.Println("FAIL: unexpected field", n)
			os.Exit(1)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		fmt.Println("FAIL: missing fields", want)
		os.Exit(1)
	}
	fmt.Println("PASS")
}
