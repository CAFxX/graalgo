package main

import (
	"fmt"
	"reflect"
)

func main() {
	// Map iteration.
	m := map[string]int{"a": 1, "b": 2}
	mv := reflect.ValueOf(m)
	it := mv.MapRange()
	keys := []string{}
	for it.Next() {
		keys = append(keys, it.Key().String())
	}
	fmt.Println(len(keys))
	// SetMapIndex and MapIndex.
	mv2 := reflect.ValueOf(map[string]int{})
	mv2.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(42))
	fmt.Println(mv2.MapIndex(reflect.ValueOf("k")).Int())
}
