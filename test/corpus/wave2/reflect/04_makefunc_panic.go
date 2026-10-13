package main

import (
	"fmt"
	"reflect"
)

func main() {
	// MakeFunc with a panicking implementation: the panic must propagate.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("caught:", r)
		}
	}()
	ft := reflect.TypeOf(func(int) int { return 0 })
	fn := reflect.MakeFunc(ft, func(args []reflect.Value) []reflect.Value {
		panic("boom")
	})
	fn.Call([]reflect.Value{reflect.ValueOf(1)})
}
