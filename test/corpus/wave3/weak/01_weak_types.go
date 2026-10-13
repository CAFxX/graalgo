package main

import (
	"fmt"
	"weak"
)

// SCOPE-LIMITED (see 00_weak.go.scopelimited): gc leg only.
func main() {
	// Canonical per pointer identity, not pointee value.
	a := 1
	b := 1
	fmt.Println(weak.Make(&a) == weak.Make(&b)) // false: distinct objects

	// Strings.
	s := "hello"
	sp := weak.Make(&s)
	fmt.Println(sp.Value() != nil && *sp.Value() == "hello")

	// Arrays.
	arr := [2]int{1, 2}
	ap := weak.Make(&arr)
	fmt.Println((*ap.Value())[1] == 2)

	// Structs.
	type S struct {
		x int
		y string
	}
	v := S{x: 3, y: "z"}
	vp := weak.Make(&v)
	fmt.Println(vp.Value().x == 3 && vp.Value().y == "z")

	// Bools.
	bb := true
	bp := weak.Make(&bb)
	fmt.Println(*bp.Value())

	// Two pointers to the same object stay equal across separate Makes.
	fmt.Println(weak.Make(&v) == weak.Make(&v))

	fmt.Println("ok")
}
