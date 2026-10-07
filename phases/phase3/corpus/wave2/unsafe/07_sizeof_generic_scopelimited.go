package main

import (
	"fmt"
	"unsafe"
)

// 07: Sizeof over a type parameter. Valid Go (gc builds and runs it), but
// go/types' gcSizes asserts on bare type parameters, so the frontend
// fails fast with a named M8 scope error instead of miscompiling.
func gsize[T any](v T) uintptr {
	return unsafe.Sizeof(v)
}

func main() {
	fmt.Println(gsize(42))
}
