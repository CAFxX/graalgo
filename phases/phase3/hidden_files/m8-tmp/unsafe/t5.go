package main

import (
	"fmt"
	"unsafe"
)

func main() {
	var p unsafe.Pointer = unsafe.Pointer(nil)
	fmt.Println(p == nil)
	var _ unsafe.Pointer = unsafe.Pointer(uintptr(0))
	fmt.Println("ok")
}
