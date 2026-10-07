package main

import (
	"fmt"
	"reflect"
)

type T struct {
	a int
	B string
}

func main() {
	// Unexported field write via Set panics.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	v := reflect.ValueOf(&T{}).Elem()
	v.Field(0).SetInt(42)
}
