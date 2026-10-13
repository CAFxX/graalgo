package main

import "reflect"

func main() {
	// ArrayOf is a named fail-fast (needs type synthesis).
	_ = reflect.ArrayOf(3, reflect.TypeOf(0))
}
