package main

import (
	"fmt"
	"reflect"
)

type T struct {
	A int
	b string
}

func main() {
	t := reflect.TypeOf(T{})
	fmt.Println(t.String())
	fmt.Println(t.NumField())
	f := t.Field(0)
	fmt.Println(f.Name, f.Type.String(), f.Tag)
	v := reflect.ValueOf(T{A: 42, b: "x"})
	fmt.Println(v.Field(0).Int())
	fmt.Println(v.Field(1).String())
	// Unexported field read via Interface panics.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	_ = v.Field(1).Interface()
}
