package main

import "fmt"

// Nested array/slice printing at arbitrary depth (R2 P0-2): the old
// single-level elemElemKind threading crashed at depth >= 3 with
// ClassCastException; the kind/name chains are now depth-general.
func main() {
	a := [2][2][2]int{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}
	fmt.Println(a)
	b := [2][2][2]int64{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}
	fmt.Println(b)
	c := [2][2][2][2]uint8{{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}, {{{9, 10}, {11, 12}}, {{13, 14}, {15, 16}}}}
	fmt.Println(c)
	fmt.Printf("%#v\n", a)
	fmt.Printf("%v %T\n", a, a)
	s := [][2]int{{1, 2}, {3, 4}}
	fmt.Println(s)
	m := [2][3]int{{1, 2, 3}, {4, 5, 6}}
	fmt.Println(m)
	defer fmt.Println(a)
}
