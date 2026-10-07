package main

import (
	"fmt"
	"unsafe"
)

func gsize[T any](v T) uintptr {
	return unsafe.Sizeof(v)
}

func main() {
	fmt.Println(gsize(42))
}
