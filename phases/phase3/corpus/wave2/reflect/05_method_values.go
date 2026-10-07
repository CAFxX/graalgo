package main

import (
	"fmt"
	"reflect"
)

type T struct{ X int }

func (t T) Double() int { return t.X * 2 }

func main() {
	// Method value via MethodByName + Call.
	v := reflect.ValueOf(T{X: 21})
	m := v.MethodByName("Double")
	out := m.Call(nil)
	fmt.Println(out[0].Int())
	// Method expression via Type.Method.
	t := reflect.TypeOf(T{})
	mt, ok := t.MethodByName("Double")
	fmt.Println(mt.Name, ok)
}
