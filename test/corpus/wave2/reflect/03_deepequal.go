package main

import (
	"fmt"
	"reflect"
)

func main() {
	// DeepEqual with cycles (must terminate, must be true).
	type N struct {
		V int
		P *N
	}
	a := &N{V: 1}
	a.P = a
	b := &N{V: 1}
	b.P = b
	fmt.Println(reflect.DeepEqual(a, b))
	// DeepEqual with funcs (only both nil are equal).
	var f1, f2 func()
	fmt.Println(reflect.DeepEqual(f1, f2))
	fmt.Println(reflect.DeepEqual(f1, func() {}))
	// DeepEqual basic.
	fmt.Println(reflect.DeepEqual([]int{1, 2}, []int{1, 2}))
	fmt.Println(reflect.DeepEqual([]int{1, 2}, []int{1, 3}))
}
