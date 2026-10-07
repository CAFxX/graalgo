package main

import "reflect"

func main() {
	// Type.Methods is a named fail-fast (iter-family).
	t := reflect.TypeOf(0)
	_ = t.Methods()
}
