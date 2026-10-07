package main

import "reflect"

func main() {
	// Value.Pointer is a named fail-fast (D5 trap: no address model).
	v := reflect.ValueOf(42)
	_ = v.Pointer()
}
