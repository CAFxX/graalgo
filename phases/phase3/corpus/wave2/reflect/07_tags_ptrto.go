package main

import (
	"fmt"
	"reflect"
)

type T struct {
	A int `json:"a"`
	B string
}

func main() {
	// Struct tag lookup.
	t := reflect.TypeOf(T{})
	f, _ := t.FieldByName("A")
	tag := f.Tag
	v, ok := tag.Lookup("json")
	fmt.Println(v, ok)
	// FieldByNameFunc.
	ff, ok := t.FieldByNameFunc(func(s string) bool { return s == "B" })
	fmt.Println(ff.Name, ok)
	// PtrTo / Elem.
	pt := reflect.PtrTo(t)
	fmt.Println(pt.String())
	fmt.Println(pt.Elem() == t)
}
