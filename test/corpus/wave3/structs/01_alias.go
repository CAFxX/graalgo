package main

import (
	"fmt"
	"structs"
)

// structs.HostLayout under its real GOROOT shape (unexported field, so
// struct{} does NOT convert to it, exactly as under gc). Added by the
// P1-2 lane (2026-11-03) as the conversion proof for real GOROOT structs
// via the disk importer: aliasing the marker type and embedding the alias
// must behave like the marker itself.
type HL structs.HostLayout

type A struct {
	_ HL
	X int
}

type B struct {
	structs.HostLayout
	Y string
}

func main() {
	a := A{X: 7}
	fmt.Println(a.X)
	var z A
	fmt.Println(z.X == 0)
	b := B{Y: "hi"}
	fmt.Println(b.Y)
	// Slices and maps of alias-marked structs behave normally.
	xs := []A{{X: 1}, {X: 2}}
	fmt.Println(len(xs), xs[0].X, xs[1].X)
	m := map[string]A{"k": {X: 3}}
	fmt.Println(m["k"].X)
	// Zero-value marker literal works (unexported field is unsettable
	// but the empty literal is legal).
	var h structs.HostLayout
	fmt.Println(h == structs.HostLayout{})
}
