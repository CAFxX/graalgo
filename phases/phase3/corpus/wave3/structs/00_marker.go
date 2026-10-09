package main

import (
	"fmt"
	"structs"
)

// The structs package is marker types only: a struct embedding
// structs.HostLayout must behave exactly like the same struct without it
// (no observable API reads the marker back).
type T struct {
	_ structs.HostLayout
	X int
	Y string
}

type U struct {
	T // promoted fields through embedding
	_ structs.HostLayout
	Z float64
}

func main() {
	v := T{X: 42, Y: "hello"}
	fmt.Println(v.X, v.Y)

	var z T
	fmt.Println(z.X == 0 && z.Y == "")

	u := U{T: v, Z: 1.5}
	fmt.Println(u.X, u.Y, u.Z)

	p := &T{X: 1, Y: "p"}
	fmt.Println(p.X, p.Y)

	// slices and maps of marked structs behave normally
	xs := []T{{X: 1}, {X: 2, Y: "two"}}
	fmt.Println(len(xs), xs[0].X, xs[1].Y)
	m := map[string]T{"k": {X: 9}}
	fmt.Println(m["k"].X)

	// marker in a nested (non-first) position is still just a field
	type W struct {
		A int
		_ structs.HostLayout
	}
	w := W{A: 3}
	fmt.Println(w.A)
}
