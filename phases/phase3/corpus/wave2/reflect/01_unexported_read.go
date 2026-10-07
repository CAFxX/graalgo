package main

import (
	"fmt"
	"reflect"
)

type inner struct {
	x int
	Y string
}

type Outer struct {
	I inner
	Z float64
}

func main() {
	v := reflect.ValueOf(Outer{I: inner{x: 7, Y: "y"}, Z: 1.5})
	// Nested unexported field (inner.x) read via Interface panics.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic1:", r)
		}
	}()
	_ = v.Field(0).Field(0).Interface()
	fmt.Println("no panic (unexpected)")
}
